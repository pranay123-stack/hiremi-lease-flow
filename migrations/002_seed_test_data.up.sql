-- Seed a test lease in "approved" state for manual/integration testing
INSERT INTO leases (id, tenant_id, property_id, rent_amount, deposit_amount, status, created_at, updated_at, version)
VALUES (
    'lease-001',
    'tenant-001',
    'property-001',
    150000,   -- 150,000 XOF monthly rent
    300000,   -- 300,000 XOF deposit (2 months)
    'approved',
    NOW(),
    NOW(),
    0
) ON CONFLICT (id) DO NOTHING;
