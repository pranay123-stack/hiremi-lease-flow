package main

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/pranay123-stack/hiremi-lease-flow/internal/events"
	"github.com/pranay123-stack/hiremi-lease-flow/internal/handler"
	"github.com/pranay123-stack/hiremi-lease-flow/internal/provider/moov"
	"github.com/pranay123-stack/hiremi-lease-flow/internal/provider/mtn"
	"github.com/pranay123-stack/hiremi-lease-flow/internal/repository"
	"github.com/pranay123-stack/hiremi-lease-flow/internal/service"
	"github.com/pranay123-stack/hiremi-lease-flow/internal/worker"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	slog.SetDefault(logger)

	cfg := loadConfig()

	// Database connection
	pool, err := pgxpool.New(context.Background(), cfg.DatabaseURL)
	if err != nil {
		slog.Error("failed to connect to database", "error", err)
		os.Exit(1)
	}
	defer pool.Close()

	if err := pool.Ping(context.Background()); err != nil {
		slog.Error("failed to ping database", "error", err)
		os.Exit(1)
	}
	slog.Info("connected to database")

	// Initialize dependencies
	eventPublisher := events.NewPostgresOutbox(pool)
	leaseRepo := repository.NewLeaseRepository(pool)
	paymentRepo := repository.NewPaymentRepository(pool)

	mtnProvider := mtn.NewSimulator(cfg.CallbackBaseURL)
	moovProvider := moov.NewSimulator(cfg.CallbackBaseURL)

	leaseService := service.NewLeaseService(leaseRepo, paymentRepo, eventPublisher)
	paymentService := service.NewPaymentService(paymentRepo, leaseRepo, eventPublisher, mtnProvider, moovProvider)

	// Router
	r := chi.NewRouter()
	r.Use(middleware.RequestID)
	r.Use(middleware.RealIP)
	r.Use(middleware.Recoverer)
	r.Use(middleware.Timeout(30 * time.Second))

	// Health check
	r.Get("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"status":"ok"}`))
	})

	// API routes
	r.Route("/api/v1", func(r chi.Router) {
		leaseHandler := handler.NewLeaseHandler(leaseService)
		paymentHandler := handler.NewPaymentHandler(paymentService)
		callbackHandler := handler.NewCallbackHandler(paymentService)

		// Lease endpoints
		r.Get("/leases/{leaseID}", leaseHandler.GetLease)
		r.Post("/leases/{leaseID}/sign", leaseHandler.SignLease)
		r.Post("/leases/{leaseID}/abandon", leaseHandler.AbandonLease)

		// Payment endpoints
		r.Post("/leases/{leaseID}/deposit", paymentHandler.InitiateDeposit)
		r.Get("/leases/{leaseID}/deposit/status", paymentHandler.GetDepositStatus)

		// Provider callback endpoints
		r.Post("/callbacks/mtn", callbackHandler.HandleMTNCallback)
		r.Post("/callbacks/moov", callbackHandler.HandleMoovCallback)
	})

	// Provider simulator endpoints (for local testing)
	r.Route("/sim", func(r chi.Router) {
		r.Post("/mtn/collect", mtnProvider.HandleCollectRequest)
		r.Post("/moov/payment", moovProvider.HandlePaymentRequest)
	})

	// Start server
	srv := &http.Server{
		Addr:         ":" + cfg.Port,
		Handler:      r,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 30 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	// Start payment expiry worker
	workerCtx, workerCancel := context.WithCancel(context.Background())
	defer workerCancel()
	expiryWorker := worker.NewPaymentExpiry(pool, 10*time.Minute, 30*time.Second)
	go expiryWorker.Run(workerCtx)

	go func() {
		slog.Info("server starting", "port", cfg.Port)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			slog.Error("server failed", "error", err)
			os.Exit(1)
		}
	}()

	// Graceful shutdown
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit
	slog.Info("shutting down server")

	workerCancel() // stop background workers

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := srv.Shutdown(ctx); err != nil {
		slog.Error("server forced to shutdown", "error", err)
	}
	slog.Info("server stopped")
}

type config struct {
	Port            string
	DatabaseURL     string
	CallbackBaseURL string
}

func loadConfig() config {
	return config{
		Port:            getEnv("PORT", "8080"),
		DatabaseURL:     getEnv("DATABASE_URL", "postgres://hiremi:hiremi@localhost:5432/hiremi?sslmode=disable"),
		CallbackBaseURL: getEnv("CALLBACK_BASE_URL", fmt.Sprintf("http://localhost:%s/api/v1/callbacks", getEnv("PORT", "8080"))),
	}
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
