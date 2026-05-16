-- Lease: the core entity tracking the approval-to-active journey
CREATE TABLE leases (
    id            TEXT PRIMARY KEY,
    tenant_id     TEXT NOT NULL,
    property_id   TEXT NOT NULL,
    rent_amount   BIGINT NOT NULL,       -- XOF, no decimals
    deposit_amount BIGINT NOT NULL,      -- XOF
    status        TEXT NOT NULL DEFAULT 'approved',
    signed_at     TIMESTAMPTZ,
    activated_at  TIMESTAMPTZ,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    version       INTEGER NOT NULL DEFAULT 0,

    CONSTRAINT valid_status CHECK (status IN ('approved', 'deposit_pending', 'deposit_paid', 'signed', 'active', 'abandoned'))
);

CREATE INDEX idx_leases_tenant ON leases(tenant_id);
CREATE INDEX idx_leases_status ON leases(status);

-- Payments: tracks each deposit payment attempt
CREATE TABLE payments (
    id              TEXT PRIMARY KEY,
    lease_id        TEXT NOT NULL REFERENCES leases(id),
    tenant_id       TEXT NOT NULL,
    amount          BIGINT NOT NULL,
    provider        TEXT NOT NULL,
    provider_tx_id  TEXT NOT NULL UNIQUE,
    phone_number    TEXT NOT NULL,
    status          TEXT NOT NULL DEFAULT 'pending',
    failure_reason  TEXT,
    callback_payload JSONB,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    version         INTEGER NOT NULL DEFAULT 0,

    CONSTRAINT valid_payment_status CHECK (status IN ('pending', 'success', 'failed', 'expired')),
    CONSTRAINT valid_provider CHECK (provider IN ('mtn_momo', 'moov_money'))
);

CREATE INDEX idx_payments_lease ON payments(lease_id);
CREATE INDEX idx_payments_provider_tx ON payments(provider_tx_id);

-- Lease signatures: legally defensible signing record
CREATE TABLE lease_signatures (
    id             TEXT PRIMARY KEY,
    lease_id       TEXT NOT NULL REFERENCES leases(id),
    tenant_id      TEXT NOT NULL,
    document_hash  TEXT NOT NULL,        -- SHA-256 of lease doc at signing time
    ip_address     TEXT NOT NULL,
    user_agent     TEXT NOT NULL,
    device_id      TEXT,
    timestamp      TIMESTAMPTZ NOT NULL,
    consent_text   TEXT NOT NULL,

    CONSTRAINT one_signature_per_lease UNIQUE (lease_id)
);

-- Rent schedules: created when lease activates
CREATE TABLE rent_schedules (
    id         TEXT PRIMARY KEY,
    lease_id   TEXT NOT NULL REFERENCES leases(id),
    tenant_id  TEXT NOT NULL,
    amount     BIGINT NOT NULL,
    due_date   DATE NOT NULL,
    period     INTEGER NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_rent_schedules_lease ON rent_schedules(lease_id);

-- Outbox events: transactional outbox for guaranteed event delivery
CREATE TABLE outbox_events (
    id            TEXT PRIMARY KEY,
    aggregate_id  TEXT NOT NULL,
    event_type    TEXT NOT NULL,
    payload       JSONB NOT NULL,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    published     BOOLEAN NOT NULL DEFAULT FALSE,
    published_at  TIMESTAMPTZ
);

CREATE INDEX idx_outbox_unpublished ON outbox_events(published, created_at) WHERE published = FALSE;
CREATE INDEX idx_outbox_aggregate ON outbox_events(aggregate_id);
