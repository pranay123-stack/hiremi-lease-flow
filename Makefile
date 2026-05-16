.PHONY: up down build run test migrate-up migrate-down clean

# Single command to start everything
up:
	docker compose up --build -d

down:
	docker compose down

# Remove volumes too
clean:
	docker compose down -v

build:
	go build -o bin/server ./cmd/server

run:
	go run ./cmd/server

test:
	go test ./... -v -race

# Run migrations locally (requires postgres running)
migrate-up:
	migrate -path migrations -database "postgres://hiremi:hiremi@localhost:5432/hiremi?sslmode=disable" up

migrate-down:
	migrate -path migrations -database "postgres://hiremi:hiremi@localhost:5432/hiremi?sslmode=disable" down

# Tail logs
logs:
	docker compose logs -f app
