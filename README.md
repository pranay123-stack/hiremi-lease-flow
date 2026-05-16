# Hire Mi — Lease Activation Flow

Backend service implementing the journey from an approved lease to an active tenancy, including mobile money deposit payment (MTN MoMo and Moov Money), lease signing with integrity proof, and first rent schedule creation.

## Quick Start

**Prerequisites:** Docker and Docker Compose installed.

```bash
# Start everything (PostgreSQL + migrations + app)
make up

# Check it's running
curl http://localhost:8080/health
```

That's it. One command.

## API Endpoints

### Lease

| Method | Endpoint | Description |
|--------|----------|-------------|
| GET | `/api/v1/leases/{leaseID}` | Get lease details and current state |
| POST | `/api/v1/leases/{leaseID}/sign` | Sign the lease (requires deposit_paid state) |
| POST | `/api/v1/leases/{leaseID}/deposit` | Initiate deposit payment |
| GET | `/api/v1/leases/{leaseID}/deposit/status` | Check deposit payment status |

### Provider Callbacks (called by payment providers)

| Method | Endpoint | Description |
|--------|----------|-------------|
| POST | `/api/v1/callbacks/mtn` | MTN MoMo payment callback |
| POST | `/api/v1/callbacks/moov` | Moov Money payment callback |

## Example Flow

```bash
# 1. Check the test lease (seeded on startup)
curl http://localhost:8080/api/v1/leases/lease-001

# 2. Initiate deposit payment via MTN MoMo
curl -X POST http://localhost:8080/api/v1/leases/lease-001/deposit \
  -H "Content-Type: application/json" \
  -d '{"tenant_id": "tenant-001", "provider": "mtn_momo", "phone_number": "+22990001234"}'

# 3. Wait 2-3 seconds for the simulated callback, then check status
curl http://localhost:8080/api/v1/leases/lease-001/deposit/status

# 4. If deposit succeeded, sign the lease
curl -X POST http://localhost:8080/api/v1/leases/lease-001/sign \
  -H "Content-Type: application/json" \
  -d '{"tenant_id": "tenant-001", "device_id": "mobile-app-v1"}'

# 5. Verify lease is now active
curl http://localhost:8080/api/v1/leases/lease-001
```

## Running Tests

```bash
go test ./... -v -race
```

## Project Structure

```
├── cmd/server/          # Application entrypoint
├── internal/
│   ├── model/           # Domain models and state machine
│   ├── repository/      # PostgreSQL data access
│   ├── service/         # Business logic (lease + payment orchestration)
│   ├── handler/         # HTTP handlers (Chi routes)
│   ├── events/          # Transactional outbox event publisher
│   └── provider/
│       ├── mtn/         # MTN MoMo simulator
│       └── moov/        # Moov Money simulator
├── migrations/          # SQL migrations (golang-migrate format)
├── docker-compose.yml   # Single-command infrastructure
├── Dockerfile           # Multi-stage build
└── Makefile             # Developer shortcuts
```

## Other Commands

```bash
make down       # Stop services
make clean      # Stop + remove database volume
make logs       # Tail application logs
make test       # Run tests
```
