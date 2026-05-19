package internal

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/pranay123-stack/hiremi-lease-flow/internal/events"
	"github.com/pranay123-stack/hiremi-lease-flow/internal/handler"
	"github.com/pranay123-stack/hiremi-lease-flow/internal/provider/moov"
	"github.com/pranay123-stack/hiremi-lease-flow/internal/provider/mtn"
	"github.com/pranay123-stack/hiremi-lease-flow/internal/repository"
	"github.com/pranay123-stack/hiremi-lease-flow/internal/service"
)

func testDB(t *testing.T) *pgxpool.Pool {
	t.Helper()
	dbURL := os.Getenv("TEST_DATABASE_URL")
	if dbURL == "" {
		dbURL = "postgres://hiremi:hiremi@localhost:5433/hiremi?sslmode=disable"
	}
	pool, err := pgxpool.New(context.Background(), dbURL)
	if err != nil {
		t.Skipf("skipping integration test: cannot connect to database: %v", err)
	}
	if err := pool.Ping(context.Background()); err != nil {
		t.Skipf("skipping integration test: cannot ping database: %v", err)
	}
	return pool
}

func seedLease(t *testing.T, pool *pgxpool.Pool, leaseID, tenantID string) {
	t.Helper()
	ctx := context.Background()
	pool.Exec(ctx, "DELETE FROM outbox_events WHERE aggregate_id = $1", leaseID)
	pool.Exec(ctx, "DELETE FROM rent_schedules WHERE lease_id = $1", leaseID)
	pool.Exec(ctx, "DELETE FROM lease_signatures WHERE lease_id = $1", leaseID)
	pool.Exec(ctx, "DELETE FROM payments WHERE lease_id = $1", leaseID)
	pool.Exec(ctx, "DELETE FROM leases WHERE id = $1", leaseID)

	_, err := pool.Exec(ctx,
		`INSERT INTO leases (id, tenant_id, property_id, rent_amount, deposit_amount, status, created_at, updated_at, version)
		 VALUES ($1, $2, 'prop-test', 100000, 200000, 'approved', NOW(), NOW(), 0)`,
		leaseID, tenantID,
	)
	if err != nil {
		t.Fatalf("seed lease: %v", err)
	}
}

type testServer struct {
	router         *chi.Mux
	leaseService   *service.LeaseService
	paymentService *service.PaymentService
}

func setupTestServer(t *testing.T, pool *pgxpool.Pool) *testServer {
	t.Helper()

	publisher := events.NewPostgresOutbox(pool)
	leaseRepo := repository.NewLeaseRepository(pool)
	paymentRepo := repository.NewPaymentRepository(pool)

	mtnSim := mtn.NewSimulator("http://localhost:0") // dummy, we send callbacks manually
	moovSim := moov.NewSimulator("http://localhost:0")

	leaseService := service.NewLeaseService(leaseRepo, paymentRepo, publisher)
	paymentService := service.NewPaymentService(paymentRepo, leaseRepo, publisher, mtnSim, moovSim)

	r := chi.NewRouter()
	leaseHandler := handler.NewLeaseHandler(leaseService)
	paymentHandler := handler.NewPaymentHandler(paymentService)
	callbackHandler := handler.NewCallbackHandler(paymentService)

	r.Get("/api/v1/leases/{leaseID}", leaseHandler.GetLease)
	r.Post("/api/v1/leases/{leaseID}/sign", leaseHandler.SignLease)
	r.Post("/api/v1/leases/{leaseID}/abandon", leaseHandler.AbandonLease)
	r.Post("/api/v1/leases/{leaseID}/deposit", paymentHandler.InitiateDeposit)
	r.Get("/api/v1/leases/{leaseID}/deposit/status", paymentHandler.GetDepositStatus)
	r.Post("/api/v1/callbacks/mtn", callbackHandler.HandleMTNCallback)
	r.Post("/api/v1/callbacks/moov", callbackHandler.HandleMoovCallback)

	return &testServer{router: r, leaseService: leaseService, paymentService: paymentService}
}

func (ts *testServer) do(method, path string, body any) *httptest.ResponseRecorder {
	var buf bytes.Buffer
	if body != nil {
		json.NewEncoder(&buf).Encode(body)
	}
	req := httptest.NewRequest(method, path, &buf)
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	ts.router.ServeHTTP(rr, req)
	return rr
}

func hmacSignBody(body []byte) string {
	mac := hmac.New(sha256.New, []byte(mtn.SimulatedAPIKey))
	mac.Write(body)
	return hex.EncodeToString(mac.Sum(nil))
}

func (ts *testServer) mtnCallback(externalID, status, reason string) *httptest.ResponseRecorder {
	callback := mtn.CallbackPayload{
		ReferenceID: "mtn-ref-test",
		ExternalID:  externalID,
		Amount:      "200000",
		Currency:    "XOF",
		Status:      status,
		Reason:      reason,
	}
	body, _ := json.Marshal(callback)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/callbacks/mtn", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Callback-Signature", hmacSignBody(body))
	rr := httptest.NewRecorder()
	ts.router.ServeHTTP(rr, req)
	return rr
}

func (ts *testServer) moovCallback(reference, statusCode, message string) *httptest.ResponseRecorder {
	callback := moov.CallbackPayload{
		TransactionID: "MOOV-test-123",
		Reference:     reference,
		Amount:        200000,
		Currency:      "XOF",
		StatusCode:    statusCode,
		Message:       message,
		CompletedAt:   time.Now().UTC().Format(time.RFC3339),
	}
	body, _ := json.Marshal(callback)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/callbacks/moov", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+moov.SimulatedBearerToken)
	rr := httptest.NewRecorder()
	ts.router.ServeHTTP(rr, req)
	return rr
}

func getLeaseStatus(t *testing.T, ts *testServer, leaseID string) string {
	t.Helper()
	rr := ts.do("GET", "/api/v1/leases/"+leaseID, nil)
	if rr.Code != http.StatusOK {
		t.Fatalf("get lease: unexpected status %d: %s", rr.Code, rr.Body.String())
	}
	var resp map[string]any
	json.Unmarshal(rr.Body.Bytes(), &resp)
	return resp["status"].(string)
}

func getProviderTxID(t *testing.T, ts *testServer, leaseID string) string {
	t.Helper()
	rr := ts.do("GET", "/api/v1/leases/"+leaseID+"/deposit/status", nil)
	if rr.Code != http.StatusOK {
		t.Fatalf("get deposit status: unexpected status %d: %s", rr.Code, rr.Body.String())
	}
	var resp map[string]any
	json.Unmarshal(rr.Body.Bytes(), &resp)
	return resp["provider_tx_id"].(string)
}

// ============================================================================
// Integration Tests
// ============================================================================

// TestHappyPath_MTN tests the full flow: approved -> deposit -> sign -> active via MTN MoMo
func TestHappyPath_MTN(t *testing.T) {
	pool := testDB(t)
	defer pool.Close()
	ts := setupTestServer(t, pool)

	leaseID := "test-happy-mtn"
	tenantID := "tenant-happy-mtn"
	seedLease(t, pool, leaseID, tenantID)

	// 1. Initiate deposit
	rr := ts.do("POST", "/api/v1/leases/"+leaseID+"/deposit", map[string]string{
		"tenant_id":    tenantID,
		"provider":     "mtn_momo",
		"phone_number": "+22990001111",
	})
	if rr.Code != http.StatusAccepted {
		t.Fatalf("initiate deposit: expected 202, got %d: %s", rr.Code, rr.Body.String())
	}

	if status := getLeaseStatus(t, ts, leaseID); status != "deposit_pending" {
		t.Fatalf("expected deposit_pending, got %s", status)
	}

	// 2. Simulate successful MTN callback
	providerTxID := getProviderTxID(t, ts, leaseID)
	rr = ts.mtnCallback(providerTxID, mtn.StatusSuccessful, "")
	if rr.Code != http.StatusOK {
		t.Fatalf("mtn callback: expected 200, got %d", rr.Code)
	}

	if status := getLeaseStatus(t, ts, leaseID); status != "deposit_paid" {
		t.Fatalf("expected deposit_paid, got %s", status)
	}

	// 3. Sign lease
	rr = ts.do("POST", "/api/v1/leases/"+leaseID+"/sign", map[string]string{
		"tenant_id": tenantID,
		"device_id": "test-device",
	})
	if rr.Code != http.StatusOK {
		t.Fatalf("sign lease: expected 200, got %d: %s", rr.Code, rr.Body.String())
	}

	if status := getLeaseStatus(t, ts, leaseID); status != "active" {
		t.Fatalf("expected active, got %s", status)
	}

	// 4. Verify events were created
	var count int
	pool.QueryRow(context.Background(),
		"SELECT COUNT(*) FROM outbox_events WHERE aggregate_id = $1", leaseID).Scan(&count)
	if count != 4 { // initiated, paid, signed, activated
		t.Fatalf("expected 4 events, got %d", count)
	}

	// 5. Verify rent schedule was created
	pool.QueryRow(context.Background(),
		"SELECT COUNT(*) FROM rent_schedules WHERE lease_id = $1", leaseID).Scan(&count)
	if count != 1 {
		t.Fatalf("expected 1 rent schedule, got %d", count)
	}

	// 6. Verify signature record exists
	pool.QueryRow(context.Background(),
		"SELECT COUNT(*) FROM lease_signatures WHERE lease_id = $1", leaseID).Scan(&count)
	if count != 1 {
		t.Fatalf("expected 1 signature, got %d", count)
	}
}

// TestHappyPath_Moov tests the full flow via Moov Money
func TestHappyPath_Moov(t *testing.T) {
	pool := testDB(t)
	defer pool.Close()
	ts := setupTestServer(t, pool)

	leaseID := "test-happy-moov"
	tenantID := "tenant-happy-moov"
	seedLease(t, pool, leaseID, tenantID)

	rr := ts.do("POST", "/api/v1/leases/"+leaseID+"/deposit", map[string]string{
		"tenant_id":    tenantID,
		"provider":     "moov_money",
		"phone_number": "+22990002222",
	})
	if rr.Code != http.StatusAccepted {
		t.Fatalf("initiate deposit: expected 202, got %d: %s", rr.Code, rr.Body.String())
	}

	providerTxID := getProviderTxID(t, ts, leaseID)
	rr = ts.moovCallback(providerTxID, moov.StatusCompleted, "")
	if rr.Code != http.StatusOK {
		t.Fatalf("moov callback: expected 200, got %d", rr.Code)
	}

	if status := getLeaseStatus(t, ts, leaseID); status != "deposit_paid" {
		t.Fatalf("expected deposit_paid, got %s", status)
	}

	rr = ts.do("POST", "/api/v1/leases/"+leaseID+"/sign", map[string]string{
		"tenant_id": tenantID,
	})
	if rr.Code != http.StatusOK {
		t.Fatalf("sign lease: expected 200, got %d: %s", rr.Code, rr.Body.String())
	}

	if status := getLeaseStatus(t, ts, leaseID); status != "active" {
		t.Fatalf("expected active, got %s", status)
	}
}

// TestFailedPayment_RetrySucceeds tests payment failure then successful retry
func TestFailedPayment_RetrySucceeds(t *testing.T) {
	pool := testDB(t)
	defer pool.Close()
	ts := setupTestServer(t, pool)

	leaseID := "test-retry"
	tenantID := "tenant-retry"
	seedLease(t, pool, leaseID, tenantID)

	// First attempt
	rr := ts.do("POST", "/api/v1/leases/"+leaseID+"/deposit", map[string]string{
		"tenant_id":    tenantID,
		"provider":     "mtn_momo",
		"phone_number": "+22990003333",
	})
	if rr.Code != http.StatusAccepted {
		t.Fatalf("first deposit: expected 202, got %d", rr.Code)
	}

	providerTxID := getProviderTxID(t, ts, leaseID)

	// Simulate failure
	ts.mtnCallback(providerTxID, mtn.StatusFailed, "NOT_ENOUGH_FUNDS")

	if status := getLeaseStatus(t, ts, leaseID); status != "approved" {
		t.Fatalf("expected lease back to approved after failure, got %s", status)
	}

	// Retry with different provider
	rr = ts.do("POST", "/api/v1/leases/"+leaseID+"/deposit", map[string]string{
		"tenant_id":    tenantID,
		"provider":     "moov_money",
		"phone_number": "+22990003333",
	})
	if rr.Code != http.StatusAccepted {
		t.Fatalf("retry deposit: expected 202, got %d: %s", rr.Code, rr.Body.String())
	}

	providerTxID = getProviderTxID(t, ts, leaseID)
	ts.moovCallback(providerTxID, moov.StatusCompleted, "")

	if status := getLeaseStatus(t, ts, leaseID); status != "deposit_paid" {
		t.Fatalf("expected deposit_paid after retry, got %s", status)
	}
}

// TestDuplicateCallback_Idempotent verifies duplicate callbacks are ignored
func TestDuplicateCallback_Idempotent(t *testing.T) {
	pool := testDB(t)
	defer pool.Close()
	ts := setupTestServer(t, pool)

	leaseID := "test-duplicate"
	tenantID := "tenant-duplicate"
	seedLease(t, pool, leaseID, tenantID)

	ts.do("POST", "/api/v1/leases/"+leaseID+"/deposit", map[string]string{
		"tenant_id":    tenantID,
		"provider":     "mtn_momo",
		"phone_number": "+22990004444",
	})

	providerTxID := getProviderTxID(t, ts, leaseID)

	// First callback — success
	rr := ts.mtnCallback(providerTxID, mtn.StatusSuccessful, "")
	if rr.Code != http.StatusOK {
		t.Fatalf("first callback: expected 200, got %d", rr.Code)
	}

	if status := getLeaseStatus(t, ts, leaseID); status != "deposit_paid" {
		t.Fatalf("expected deposit_paid, got %s", status)
	}

	// Duplicate callback — should be ignored, no error
	rr = ts.mtnCallback(providerTxID, mtn.StatusSuccessful, "")
	if rr.Code != http.StatusOK {
		t.Fatalf("duplicate callback: expected 200, got %d", rr.Code)
	}

	// Conflicting callback (failure after success) — should also be ignored
	rr = ts.mtnCallback(providerTxID, mtn.StatusFailed, "SOME_ERROR")
	if rr.Code != http.StatusOK {
		t.Fatalf("conflicting callback: expected 200, got %d", rr.Code)
	}

	// Lease should still be deposit_paid
	if status := getLeaseStatus(t, ts, leaseID); status != "deposit_paid" {
		t.Fatalf("expected deposit_paid unchanged, got %s", status)
	}
}

// TestSignBeforeDeposit_Rejected verifies signing is blocked without payment
func TestSignBeforeDeposit_Rejected(t *testing.T) {
	pool := testDB(t)
	defer pool.Close()
	ts := setupTestServer(t, pool)

	leaseID := "test-sign-early"
	tenantID := "tenant-sign-early"
	seedLease(t, pool, leaseID, tenantID)

	rr := ts.do("POST", "/api/v1/leases/"+leaseID+"/sign", map[string]string{
		"tenant_id": tenantID,
	})
	if rr.Code != http.StatusConflict {
		t.Fatalf("expected 409 conflict, got %d: %s", rr.Code, rr.Body.String())
	}

	// Lease should still be approved
	if status := getLeaseStatus(t, ts, leaseID); status != "approved" {
		t.Fatalf("expected approved, got %s", status)
	}
}

// TestAbandonLease tests abandoning from various states
func TestAbandonLease(t *testing.T) {
	pool := testDB(t)
	defer pool.Close()
	ts := setupTestServer(t, pool)

	leaseID := "test-abandon"
	tenantID := "tenant-abandon"
	seedLease(t, pool, leaseID, tenantID)

	rr := ts.do("POST", "/api/v1/leases/"+leaseID+"/abandon", map[string]string{
		"tenant_id": tenantID,
	})
	if rr.Code != http.StatusOK {
		t.Fatalf("abandon: expected 200, got %d: %s", rr.Code, rr.Body.String())
	}

	if status := getLeaseStatus(t, ts, leaseID); status != "abandoned" {
		t.Fatalf("expected abandoned, got %s", status)
	}

	// Cannot abandon again
	rr = ts.do("POST", "/api/v1/leases/"+leaseID+"/abandon", map[string]string{
		"tenant_id": tenantID,
	})
	if rr.Code != http.StatusConflict {
		t.Fatalf("double abandon: expected 409, got %d: %s", rr.Code, rr.Body.String())
	}
}

// TestDuplicatePendingDeposit_Rejected verifies only one pending deposit at a time
func TestDuplicatePendingDeposit_Rejected(t *testing.T) {
	pool := testDB(t)
	defer pool.Close()
	ts := setupTestServer(t, pool)

	leaseID := "test-dup-deposit"
	tenantID := "tenant-dup-deposit"
	seedLease(t, pool, leaseID, tenantID)

	body := map[string]string{
		"tenant_id":    tenantID,
		"provider":     "mtn_momo",
		"phone_number": "+22990005555",
	}

	rr := ts.do("POST", "/api/v1/leases/"+leaseID+"/deposit", body)
	if rr.Code != http.StatusAccepted {
		t.Fatalf("first deposit: expected 202, got %d", rr.Code)
	}

	// Second deposit while first is still pending
	rr = ts.do("POST", "/api/v1/leases/"+leaseID+"/deposit", body)
	if rr.Code != http.StatusConflict {
		t.Fatalf("duplicate pending deposit: expected 409, got %d: %s", rr.Code, rr.Body.String())
	}
}

// TestWrongTenant_Rejected verifies tenant ownership checks
func TestWrongTenant_Rejected(t *testing.T) {
	pool := testDB(t)
	defer pool.Close()
	ts := setupTestServer(t, pool)

	leaseID := "test-wrong-tenant"
	tenantID := "tenant-correct"
	seedLease(t, pool, leaseID, tenantID)

	rr := ts.do("POST", "/api/v1/leases/"+leaseID+"/deposit", map[string]string{
		"tenant_id":    "tenant-wrong",
		"provider":     "mtn_momo",
		"phone_number": "+22990006666",
	})
	if rr.Code != http.StatusInternalServerError {
		t.Logf("wrong tenant deposit: got status %d (expected rejection)", rr.Code)
	}
	// The lease should not have changed
	if status := getLeaseStatus(t, ts, leaseID); status != "approved" {
		t.Fatalf("expected approved, got %s", status)
	}
}

// TestMTNCallback_InvalidSignature verifies HMAC verification
func TestMTNCallback_InvalidSignature(t *testing.T) {
	pool := testDB(t)
	defer pool.Close()
	ts := setupTestServer(t, pool)

	body, _ := json.Marshal(mtn.CallbackPayload{
		ExternalID: "some-id",
		Status:     mtn.StatusSuccessful,
	})

	req := httptest.NewRequest(http.MethodPost, "/api/v1/callbacks/mtn", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Callback-Signature", "invalid-signature")

	rr := httptest.NewRecorder()
	ts.router.ServeHTTP(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("invalid MTN signature: expected 401, got %d", rr.Code)
	}
}

// TestMoovCallback_InvalidToken verifies bearer token verification
func TestMoovCallback_InvalidToken(t *testing.T) {
	pool := testDB(t)
	defer pool.Close()
	ts := setupTestServer(t, pool)

	body, _ := json.Marshal(moov.CallbackPayload{
		Reference:  "some-ref",
		StatusCode: moov.StatusCompleted,
	})

	req := httptest.NewRequest(http.MethodPost, "/api/v1/callbacks/moov", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer wrong-token")

	rr := httptest.NewRecorder()
	ts.router.ServeHTTP(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("invalid Moov token: expected 401, got %d", rr.Code)
	}
}
