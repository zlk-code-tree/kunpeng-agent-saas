-- Dify 实例支持平台共享和租户独享；健康状态参与路由，Worker 不会把新任务发送到不可用实例。
ALTER TABLE dify_instances
    ADD COLUMN IF NOT EXISTS deployment_type VARCHAR(16) NOT NULL DEFAULT 'dedicated'
        CHECK (deployment_type IN ('shared','dedicated')),
    ADD COLUMN IF NOT EXISTS health_status VARCHAR(16) NOT NULL DEFAULT 'unknown'
        CHECK (health_status IN ('unknown','healthy','degraded','unavailable')),
    ADD COLUMN IF NOT EXISTS last_health_check_at TIMESTAMPTZ,
    ADD COLUMN IF NOT EXISTS consecutive_failures INTEGER NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS credential_version INTEGER NOT NULL DEFAULT 1;

UPDATE dify_instances
SET deployment_type=CASE WHEN is_shared THEN 'shared' ELSE 'dedicated' END;

CREATE TABLE IF NOT EXISTS model_providers (
    id UUID PRIMARY KEY,
    tenant_id UUID REFERENCES tenants(id),
    code VARCHAR(64) NOT NULL,
    name VARCHAR(100) NOT NULL,
    provider_type VARCHAR(32) NOT NULL CHECK (provider_type IN ('openai','anthropic','deepseek','ollama','vllm','compatible')),
    base_url TEXT NOT NULL,
    encrypted_credentials BYTEA,
    status VARCHAR(32) NOT NULL DEFAULT 'active' CHECK (status IN ('active','disabled','unavailable')),
    is_shared BOOLEAN NOT NULL DEFAULT FALSE,
    health_status VARCHAR(16) NOT NULL DEFAULT 'unknown' CHECK (health_status IN ('unknown','healthy','degraded','unavailable')),
    last_health_check_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (tenant_id, code)
);

CREATE INDEX IF NOT EXISTS idx_model_providers_tenant ON model_providers (tenant_id, status);

-- 供应商价格采用只追加版本并由成本明细引用，上游价格变化后仍可复现历史毛利。
CREATE TABLE IF NOT EXISTS model_price_versions (
    id UUID PRIMARY KEY,
    provider_id UUID NOT NULL REFERENCES model_providers(id),
    model VARCHAR(128) NOT NULL,
    version INTEGER NOT NULL,
    input_per_1000 NUMERIC(24, 10) NOT NULL DEFAULT 0,
    output_per_1000 NUMERIC(24, 10) NOT NULL DEFAULT 0,
    currency VARCHAR(16) NOT NULL DEFAULT 'CNY',
    effective_at TIMESTAMPTZ NOT NULL,
    expires_at TIMESTAMPTZ,
    status VARCHAR(16) NOT NULL DEFAULT 'active' CHECK (status IN ('draft','active','retired')),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (provider_id, model, version)
);

CREATE TABLE IF NOT EXISTS agent_model_routes (
    id UUID PRIMARY KEY,
    tenant_id UUID REFERENCES tenants(id),
    agent_version_id UUID NOT NULL REFERENCES agent_versions(id),
    provider_id UUID NOT NULL REFERENCES model_providers(id),
    model VARCHAR(128) NOT NULL,
    priority INTEGER NOT NULL DEFAULT 100,
    status VARCHAR(16) NOT NULL DEFAULT 'active' CHECK (status IN ('active','disabled')),
    config JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE UNIQUE INDEX IF NOT EXISTS uq_agent_model_route_active
    ON agent_model_routes (COALESCE(tenant_id, '00000000-0000-0000-0000-000000000000'::uuid), agent_version_id, provider_id, model)
    WHERE status='active';

-- 工具定义全局共享且版本化；tenant_tool_grants 是限制租户和智能体版本调用权限的授权边界。
CREATE TABLE IF NOT EXISTS tool_catalog (
    id UUID PRIMARY KEY,
    code VARCHAR(64) NOT NULL UNIQUE,
    name VARCHAR(100) NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    protocol VARCHAR(16) NOT NULL CHECK (protocol IN ('http_json','mcp_http')),
    status VARCHAR(16) NOT NULL DEFAULT 'draft' CHECK (status IN ('draft','active','disabled')),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS tool_versions (
    id UUID PRIMARY KEY,
    tool_id UUID NOT NULL REFERENCES tool_catalog(id),
    version INTEGER NOT NULL,
    endpoint TEXT NOT NULL,
    method VARCHAR(16) NOT NULL DEFAULT 'POST',
    encrypted_credentials BYTEA,
    input_schema JSONB NOT NULL DEFAULT '{}'::jsonb,
    output_schema JSONB NOT NULL DEFAULT '{}'::jsonb,
    timeout_ms INTEGER NOT NULL DEFAULT 10000 CHECK (timeout_ms BETWEEN 100 AND 120000),
    max_response_bytes INTEGER NOT NULL DEFAULT 1048576 CHECK (max_response_bytes BETWEEN 1024 AND 10485760),
    status VARCHAR(16) NOT NULL DEFAULT 'active' CHECK (status IN ('draft','active','retired')),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (tool_id, version)
);

CREATE TABLE IF NOT EXISTS tenant_tool_grants (
    id UUID PRIMARY KEY,
    tenant_id UUID NOT NULL REFERENCES tenants(id),
    tool_id UUID NOT NULL REFERENCES tool_catalog(id),
    allowed_agent_version_ids UUID[] NOT NULL DEFAULT '{}',
    max_calls_per_run INTEGER NOT NULL DEFAULT 10 CHECK (max_calls_per_run > 0),
    status VARCHAR(16) NOT NULL DEFAULT 'active' CHECK (status IN ('active','disabled')),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (tenant_id, tool_id)
);

CREATE TABLE IF NOT EXISTS tool_invocations (
    id UUID PRIMARY KEY,
    tenant_id UUID NOT NULL REFERENCES tenants(id),
    run_id UUID NOT NULL REFERENCES ai_runs(id),
    user_id UUID NOT NULL REFERENCES users(id),
    tool_version_id UUID NOT NULL REFERENCES tool_versions(id),
    request_payload JSONB NOT NULL DEFAULT '{}'::jsonb,
    response_payload JSONB,
    status VARCHAR(16) NOT NULL CHECK (status IN ('running','succeeded','failed','timed_out','denied')),
    duration_ms BIGINT,
    error_message TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    finished_at TIMESTAMPTZ
);

CREATE INDEX IF NOT EXISTS idx_tool_invocations_run ON tool_invocations (tenant_id, run_id, created_at);

-- 客户 Credit 结算与上游供应商成本分账记录；cost_entries 保存真实基础设施成本用于毛利和对账。
CREATE TABLE IF NOT EXISTS cost_entries (
    id UUID PRIMARY KEY,
    tenant_id UUID NOT NULL REFERENCES tenants(id),
    run_id UUID REFERENCES ai_runs(id),
    usage_record_id UUID REFERENCES usage_records(id),
    cost_type VARCHAR(32) NOT NULL CHECK (cost_type IN ('model_input','model_output','embedding','rerank','image','audio','tool')),
    provider VARCHAR(64),
    model VARCHAR(128),
    quantity NUMERIC(24, 8) NOT NULL DEFAULT 0,
    unit_price NUMERIC(24, 10) NOT NULL DEFAULT 0,
    amount NUMERIC(24, 10) NOT NULL DEFAULT 0,
    currency VARCHAR(16) NOT NULL DEFAULT 'CNY',
    price_version_id UUID REFERENCES model_price_versions(id),
    source_snapshot JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (usage_record_id, cost_type)
);

CREATE INDEX IF NOT EXISTS idx_cost_entries_tenant_created ON cost_entries (tenant_id, created_at DESC);

CREATE TABLE IF NOT EXISTS reconciliation_runs (
    id UUID PRIMARY KEY,
    scope VARCHAR(32) NOT NULL CHECK (scope IN ('usage','billing','provider')),
    tenant_id UUID REFERENCES tenants(id),
    status VARCHAR(16) NOT NULL CHECK (status IN ('running','succeeded','failed')),
    report JSONB NOT NULL DEFAULT '{}'::jsonb,
    started_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    finished_at TIMESTAMPTZ
);

ALTER TABLE ai_runs
    ADD COLUMN IF NOT EXISTS lease_owner VARCHAR(128),
    ADD COLUMN IF NOT EXISTS lease_expires_at TIMESTAMPTZ,
    ADD COLUMN IF NOT EXISTS dispatch_attempts INTEGER NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS unknown_reason TEXT;

-- 运行租约过期后转为 unknown 而不自动重放，因为本地 Worker 消失时上游请求可能已经成功。
CREATE INDEX IF NOT EXISTS idx_ai_runs_recovery
    ON ai_runs (status, lease_expires_at)
    WHERE status IN ('dispatching','running','unknown');

CREATE TABLE IF NOT EXISTS tenant_exports (
    id UUID PRIMARY KEY,
    tenant_id UUID NOT NULL REFERENCES tenants(id),
    requested_by UUID REFERENCES users(id),
    status VARCHAR(16) NOT NULL CHECK (status IN ('queued','running','succeeded','failed')),
    object_key TEXT,
    checksum_sha256 VARCHAR(64),
    report JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    finished_at TIMESTAMPTZ
);

-- API Key 和 Webhook 是企业外部接入的表结构基础；秘密只保存摘要或密文，禁止明文落库。
CREATE TABLE IF NOT EXISTS api_keys (
    id UUID PRIMARY KEY,
    tenant_id UUID NOT NULL REFERENCES tenants(id),
    created_by UUID NOT NULL REFERENCES users(id),
    name VARCHAR(100) NOT NULL,
    key_prefix VARCHAR(16) NOT NULL,
    key_hash VARCHAR(64) NOT NULL UNIQUE,
    scopes TEXT[] NOT NULL DEFAULT '{}',
    status VARCHAR(16) NOT NULL DEFAULT 'active' CHECK (status IN ('active','revoked')),
    expires_at TIMESTAMPTZ,
    last_used_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS webhook_endpoints (
    id UUID PRIMARY KEY,
    tenant_id UUID NOT NULL REFERENCES tenants(id),
    created_by UUID NOT NULL REFERENCES users(id),
    url TEXT NOT NULL,
    encrypted_secret BYTEA NOT NULL,
    events TEXT[] NOT NULL DEFAULT '{}',
    status VARCHAR(16) NOT NULL DEFAULT 'active' CHECK (status IN ('active','disabled')),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS webhook_deliveries (
    id UUID PRIMARY KEY,
    endpoint_id UUID NOT NULL REFERENCES webhook_endpoints(id),
    event_type VARCHAR(128) NOT NULL,
    event_id VARCHAR(128) NOT NULL,
    payload JSONB NOT NULL,
    status VARCHAR(16) NOT NULL DEFAULT 'pending' CHECK (status IN ('pending','succeeded','failed','dead')),
    attempts INTEGER NOT NULL DEFAULT 0,
    next_attempt_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    response_status INTEGER,
    response_body TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    delivered_at TIMESTAMPTZ,
    UNIQUE (endpoint_id, event_id)
);

UPDATE installation_settings
SET schema_version=GREATEST(schema_version, 5), updated_at=NOW();
