# Design Notes

## Architecture Choices

### Go + Chi + stdlib

Chi provides a lightweight, idiomatic router that composes well with standard `net/http` middleware. No magic, no framework lock-in. The rest is stdlib: `log/slog` for structured logging, `context` for cancellation, `crypto` for signatures.

### PostgreSQL

Chosen because:
- ACID transactions are non-negotiable for a payment/lease state machine
- `FOR UPDATE` row locking prevents race conditions on concurrent callbacks
- JSONB columns store raw callback payloads for audit without schema bloat
- CHECK constraints enforce valid states at the database level (defense in depth)

### Transactional Outbox Pattern (Event Publishing)

State changes and their corresponding events are written in the **same database transaction**. This guarantees:
- Events are never published without the state change succeeding
- State changes never happen without the event being recorded
- No distributed transaction coordinator needed

A separate process (not built yet — would be a polling worker or CDC-based) picks up unpublished events and delivers them downstream. For this trial, events land in the `outbox_events` table and are available for consumption.

**Why not an external broker?** Adding Kafka/NATS for a single-feature trial adds operational complexity without proportional benefit. The outbox table gives the same guarantee with zero additional infrastructure. A real deployment would add a relay process that publishes from the outbox to whatever broker the platform uses.

### Optimistic Locking

Both `leases` and `payments` tables have a `version` column. Updates include `WHERE version = $expected` — if another process modified the row between our read and write, the update affects zero rows and we return an error. This catches:
- Duplicate callbacks racing each other
- Concurrent sign attempts
- Any unexpected parallelism

### Strict State Machine

The lease can only advance through a defined set of transitions (see `model/lease.go`). Invalid transitions return an error immediately. Combined with `FOR UPDATE` locking and version checks, the lease **cannot** reach a state it hasn't earned.

## Mobile Money Simulations

The two providers differ realistically:

| Aspect | MTN MoMo | Moov Money |
|--------|----------|------------|
| Callback auth | HMAC-SHA256 signature | Bearer token |
| Success status | `SUCCESSFUL` | `COMPLETED` |
| ID field | `externalId` | `reference` |
| Response time | 1-3s | 2-5s |
| Success rate | ~80% | ~75% |

The integration code (`service/payment.go`) handles each provider's callback shape independently. Swapping the simulator for a real client means changing only the HTTP call in `sendToProvider()` — the callback handlers remain identical.

## Unhappy Path Handling

1. **Failed payment**: Lease returns to `approved` state. Tenant can retry with same or different provider.
2. **Duplicate callback**: `IsTerminal()` check — if payment already succeeded/failed, duplicate is logged and ignored (idempotent).
3. **Late callback**: Same as duplicate — if the lease has already moved past `deposit_pending`, the callback is a no-op.
4. **Conflicting callback**: The first callback to acquire the row lock wins. The second sees the payment is already terminal and bails.
5. **Abandoned flow**: Explicit `abandon` transition available from approved, deposit_pending, or deposit_paid states.

## Signature Integrity

The signing record captures:
- **Who**: tenant_id
- **What**: SHA-256 hash of the lease document content (deterministic from lease fields)
- **When**: UTC timestamp
- **Where**: IP address, user agent, device ID
- **Consent**: The exact text the tenant agreed to

The document hash proves the lease wasn't altered after signing — recomputing the hash from current lease fields must match the stored hash.

## What I'd Do With More Time

- **Outbox relay worker**: Poll unpublished events and push to a message broker
- **Payment expiry**: Background job to expire pending payments after N minutes
- **Integration tests**: Spin up a test Postgres, run the full flow end-to-end
- **Rate limiting**: On deposit initiation to prevent abuse
- **Metrics**: Prometheus counters for state transitions, payment outcomes
- **Lease document generation**: Actual PDF with all terms, hashed for signing
- **Webhook retry logic**: Exponential backoff if our callback endpoint is temporarily down
