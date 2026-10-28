CREATE TABLE IF NOT EXISTS tenant_invitations (
    id UUID PRIMARY KEY,
    tenant_id UUID NOT NULL REFERENCES tenants(id),
    invitee_user_id UUID NOT NULL REFERENCES users(id),
    invited_by UUID NOT NULL REFERENCES users(id),
    role VARCHAR(32) NOT NULL CHECK (role IN ('admin', 'member', 'finance')),
    token_hash VARCHAR(64) NOT NULL UNIQUE,
    status VARCHAR(32) NOT NULL DEFAULT 'pending'
        CHECK (status IN ('pending', 'accepted', 'revoked', 'expired')),
    expires_at TIMESTAMPTZ NOT NULL,
    accepted_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE UNIQUE INDEX IF NOT EXISTS uq_tenant_pending_invitation
    ON tenant_invitations (tenant_id, invitee_user_id)
    WHERE status = 'pending';

CREATE TABLE IF NOT EXISTS tenant_mode_migrations (
    id UUID PRIMARY KEY,
    installation_id UUID NOT NULL REFERENCES installation_settings(id),
    from_mode VARCHAR(16) NOT NULL CHECK (from_mode IN ('single', 'multi')),
    to_mode VARCHAR(16) NOT NULL CHECK (to_mode IN ('single', 'multi')),
    status VARCHAR(32) NOT NULL CHECK (status IN ('prechecked', 'running', 'succeeded', 'failed')),
    report JSONB NOT NULL DEFAULT '{}'::jsonb,
    error_message TEXT,
    started_at TIMESTAMPTZ,
    finished_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS billing_accounts (
    id UUID PRIMARY KEY,
    tenant_id UUID NOT NULL REFERENCES tenants(id),
    subject_type VARCHAR(16) NOT NULL CHECK (subject_type IN ('user', 'tenant')),
    subject_id UUID NOT NULL,
    status VARCHAR(32) NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'frozen', 'closed')),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (subject_type, subject_id)
);

CREATE INDEX IF NOT EXISTS idx_billing_accounts_tenant ON billing_accounts (tenant_id, status);

CREATE TABLE IF NOT EXISTS wallet_accounts (
    id UUID PRIMARY KEY,
    billing_account_id UUID NOT NULL UNIQUE REFERENCES billing_accounts(id),
    currency VARCHAR(16) NOT NULL DEFAULT 'credit',
    balance NUMERIC(24, 8) NOT NULL DEFAULT 0 CHECK (balance >= 0),
    frozen_amount NUMERIC(24, 8) NOT NULL DEFAULT 0 CHECK (frozen_amount >= 0),
    version BIGINT NOT NULL DEFAULT 0,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CHECK (balance >= frozen_amount)
);

CREATE TABLE IF NOT EXISTS wallet_ledger (
    id UUID PRIMARY KEY,
    tenant_id UUID NOT NULL REFERENCES tenants(id),
    billing_account_id UUID NOT NULL REFERENCES billing_accounts(id),
    wallet_account_id UUID NOT NULL REFERENCES wallet_accounts(id),
    entry_type VARCHAR(32) NOT NULL CHECK (entry_type IN ('recharge', 'grant', 'consume', 'refund', 'adjust', 'reserve', 'release')),
    amount NUMERIC(24, 8) NOT NULL,
    balance_after NUMERIC(24, 8) NOT NULL,
    frozen_after NUMERIC(24, 8) NOT NULL,
    business_type VARCHAR(64) NOT NULL,
    business_id UUID,
    idempotency_key VARCHAR(128) NOT NULL,
    metadata JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (billing_account_id, idempotency_key)
);

CREATE TABLE IF NOT EXISTS plans (
    id UUID PRIMARY KEY,
    code VARCHAR(64) NOT NULL UNIQUE,
    name VARCHAR(100) NOT NULL,
    audience VARCHAR(16) NOT NULL CHECK (audience IN ('personal', 'organization', 'all')),
    status VARCHAR(32) NOT NULL DEFAULT 'draft' CHECK (status IN ('draft', 'active', 'retired')),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS plan_versions (
    id UUID PRIMARY KEY,
    plan_id UUID NOT NULL REFERENCES plans(id),
    version INTEGER NOT NULL,
    billing_period VARCHAR(16) NOT NULL CHECK (billing_period IN ('month', 'year', 'one_time')),
    price NUMERIC(24, 8) NOT NULL CHECK (price >= 0),
    currency VARCHAR(16) NOT NULL DEFAULT 'CNY',
    benefits JSONB NOT NULL DEFAULT '{}'::jsonb,
    effective_at TIMESTAMPTZ NOT NULL,
    expires_at TIMESTAMPTZ,
    status VARCHAR(32) NOT NULL DEFAULT 'draft' CHECK (status IN ('draft', 'active', 'retired')),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (plan_id, version)
);

CREATE TABLE IF NOT EXISTS subscriptions (
    id UUID PRIMARY KEY,
    tenant_id UUID NOT NULL REFERENCES tenants(id),
    billing_account_id UUID NOT NULL REFERENCES billing_accounts(id),
    plan_version_id UUID NOT NULL REFERENCES plan_versions(id),
    status VARCHAR(32) NOT NULL CHECK (status IN ('trialing', 'active', 'past_due', 'canceled', 'expired')),
    current_period_start TIMESTAMPTZ NOT NULL,
    current_period_end TIMESTAMPTZ NOT NULL,
    cancel_at_period_end BOOLEAN NOT NULL DEFAULT FALSE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_subscriptions_account ON subscriptions (billing_account_id, status, current_period_end);

CREATE TABLE IF NOT EXISTS entitlements (
    id UUID PRIMARY KEY,
    tenant_id UUID NOT NULL REFERENCES tenants(id),
    subscription_id UUID NOT NULL REFERENCES subscriptions(id),
    billing_account_id UUID NOT NULL REFERENCES billing_accounts(id),
    entitlement_key VARCHAR(64) NOT NULL,
    limit_value NUMERIC(24, 8),
    config JSONB NOT NULL DEFAULT '{}'::jsonb,
    period_start TIMESTAMPTZ NOT NULL,
    period_end TIMESTAMPTZ NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (subscription_id, entitlement_key, period_start)
);

CREATE TABLE IF NOT EXISTS entitlement_usages (
    id UUID PRIMARY KEY,
    tenant_id UUID NOT NULL REFERENCES tenants(id),
    entitlement_id UUID NOT NULL REFERENCES entitlements(id),
    used_value NUMERIC(24, 8) NOT NULL DEFAULT 0 CHECK (used_value >= 0),
    reserved_value NUMERIC(24, 8) NOT NULL DEFAULT 0 CHECK (reserved_value >= 0),
    version BIGINT NOT NULL DEFAULT 0,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (entitlement_id)
);

CREATE TABLE IF NOT EXISTS price_versions (
    id UUID PRIMARY KEY,
    code VARCHAR(64) NOT NULL,
    version INTEGER NOT NULL,
    pricing JSONB NOT NULL,
    effective_at TIMESTAMPTZ NOT NULL,
    expires_at TIMESTAMPTZ,
    status VARCHAR(32) NOT NULL DEFAULT 'active' CHECK (status IN ('draft', 'active', 'retired')),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (code, version)
);

CREATE TABLE IF NOT EXISTS billing_reservations (
    id UUID PRIMARY KEY,
    tenant_id UUID NOT NULL REFERENCES tenants(id),
    billing_account_id UUID NOT NULL REFERENCES billing_accounts(id),
    run_id UUID NOT NULL UNIQUE REFERENCES ai_runs(id),
    price_version_id UUID REFERENCES price_versions(id),
    reserved_amount NUMERIC(24, 8) NOT NULL CHECK (reserved_amount >= 0),
    actual_amount NUMERIC(24, 8),
    status VARCHAR(32) NOT NULL CHECK (status IN ('reserved', 'awaiting_usage', 'settled', 'released', 'review')),
    idempotency_key VARCHAR(128) NOT NULL,
    expires_at TIMESTAMPTZ NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (billing_account_id, idempotency_key)
);

CREATE TABLE IF NOT EXISTS billing_settlements (
    id UUID PRIMARY KEY,
    tenant_id UUID NOT NULL REFERENCES tenants(id),
    billing_account_id UUID NOT NULL REFERENCES billing_accounts(id),
    reservation_id UUID NOT NULL UNIQUE REFERENCES billing_reservations(id),
    usage_record_id UUID REFERENCES usage_records(id),
    price_version_id UUID REFERENCES price_versions(id),
    amount NUMERIC(24, 8) NOT NULL CHECK (amount >= 0),
    status VARCHAR(32) NOT NULL CHECK (status IN ('settled', 'released', 'review')),
    pricing_snapshot JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS orders (
    id UUID PRIMARY KEY,
    order_no VARCHAR(64) NOT NULL UNIQUE,
    tenant_id UUID NOT NULL REFERENCES tenants(id),
    user_id UUID NOT NULL REFERENCES users(id),
    billing_account_id UUID NOT NULL REFERENCES billing_accounts(id),
    order_type VARCHAR(32) NOT NULL CHECK (order_type IN ('subscription', 'recharge')),
    plan_version_id UUID REFERENCES plan_versions(id),
    amount NUMERIC(24, 8) NOT NULL CHECK (amount >= 0),
    currency VARCHAR(16) NOT NULL DEFAULT 'CNY',
    status VARCHAR(32) NOT NULL CHECK (status IN ('pending', 'paid', 'closed', 'refunding', 'refunded', 'failed')),
    expires_at TIMESTAMPTZ,
    paid_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_orders_tenant_created ON orders (tenant_id, created_at DESC);

CREATE TABLE IF NOT EXISTS payment_transactions (
    id UUID PRIMARY KEY,
    order_id UUID NOT NULL REFERENCES orders(id),
    channel VARCHAR(32) NOT NULL,
    channel_transaction_id VARCHAR(128),
    status VARCHAR(32) NOT NULL CHECK (status IN ('created', 'processing', 'succeeded', 'failed', 'closed')),
    amount NUMERIC(24, 8) NOT NULL,
    request_payload JSONB NOT NULL DEFAULT '{}'::jsonb,
    response_payload JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (channel, channel_transaction_id)
);

CREATE TABLE IF NOT EXISTS refunds (
    id UUID PRIMARY KEY,
    order_id UUID NOT NULL REFERENCES orders(id),
    payment_transaction_id UUID REFERENCES payment_transactions(id),
    refund_no VARCHAR(64) NOT NULL UNIQUE,
    amount NUMERIC(24, 8) NOT NULL CHECK (amount > 0),
    reason TEXT,
    status VARCHAR(32) NOT NULL CHECK (status IN ('pending', 'processing', 'succeeded', 'failed', 'closed')),
    channel_refund_id VARCHAR(128),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS payment_callback_logs (
    id UUID PRIMARY KEY,
    channel VARCHAR(32) NOT NULL,
    event_id VARCHAR(128) NOT NULL,
    signature_valid BOOLEAN NOT NULL,
    payload JSONB NOT NULL,
    processing_status VARCHAR(32) NOT NULL CHECK (processing_status IN ('received', 'processed', 'ignored', 'failed')),
    error_message TEXT,
    received_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    processed_at TIMESTAMPTZ,
    UNIQUE (channel, event_id)
);

DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM pg_constraint WHERE conname = 'fk_ai_runs_billing_account'
    ) THEN
        ALTER TABLE ai_runs
            ADD CONSTRAINT fk_ai_runs_billing_account
            FOREIGN KEY (billing_account_id) REFERENCES billing_accounts(id);
    END IF;
END $$;

UPDATE installation_settings SET schema_version = GREATEST(schema_version, 2), updated_at = NOW();
