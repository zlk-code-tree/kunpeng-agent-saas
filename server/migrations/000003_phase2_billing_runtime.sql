-- 第二期计费初始化数据。套餐和价格均采用版本化记录，历史订阅和结算不能依赖可变的“当前价格”。
INSERT INTO plans (id, code, name, audience, status)
VALUES ('00000000-0000-4000-8000-000000000101', 'free', '免费试用', 'all', 'active')
ON CONFLICT (code) DO UPDATE SET name=EXCLUDED.name, audience=EXCLUDED.audience, status='active', updated_at=NOW();

INSERT INTO plan_versions (
    id, plan_id, version, billing_period, price, currency, benefits, effective_at, status
)
VALUES (
    '00000000-0000-4000-8000-000000000102',
    '00000000-0000-4000-8000-000000000101',
    1,
    'month',
    0,
    'CNY',
    '{"token_credits":100,"agent_calls":1000,"knowledge_storage_mb":512,"documents":100,"members":5}'::jsonb,
    NOW(),
    'active'
)
ON CONFLICT (plan_id, version) DO UPDATE SET benefits=EXCLUDED.benefits, status='active';

INSERT INTO price_versions (id, code, version, pricing, effective_at, status)
VALUES (
    '00000000-0000-4000-8000-000000000103',
    'default-ai',
    1,
    '{"reserve_per_run":1,"input_per_1000":0.002,"output_per_1000":0.006,"minimum":0.001}'::jsonb,
    NOW(),
    'active'
)
ON CONFLICT (code, version) DO UPDATE SET pricing=EXCLUDED.pricing, status='active';

CREATE UNIQUE INDEX IF NOT EXISTS uq_usage_records_run_source
    ON usage_records (run_id, source);

-- 只回填没有任何历史流水的钱包，使迁移保持幂等，并避免给已使用账户重复赠送额度。
UPDATE wallet_accounts w
SET balance=100, updated_at=NOW(), version=version+1
WHERE balance=0
  AND NOT EXISTS (
      SELECT 1 FROM wallet_ledger l WHERE l.wallet_account_id=w.id
  );

INSERT INTO wallet_ledger (
    id, tenant_id, billing_account_id, wallet_account_id, entry_type,
    amount, balance_after, frozen_after, business_type, idempotency_key, metadata
)
SELECT gen_random_uuid(), b.tenant_id, b.id, w.id, 'grant',
       100, w.balance, w.frozen_amount, 'phase2_initial_trial',
       'phase2-initial-trial', '{"source":"system","reason":"initial trial credit"}'::jsonb
FROM wallet_accounts w
JOIN billing_accounts b ON b.id=w.billing_account_id
WHERE w.balance=100
  AND NOT EXISTS (
      SELECT 1 FROM wallet_ledger l
      WHERE l.billing_account_id=b.id AND l.idempotency_key='phase2-initial-trial'
  );

INSERT INTO subscriptions (
    id, tenant_id, billing_account_id, plan_version_id, status,
    current_period_start, current_period_end
)
SELECT gen_random_uuid(), b.tenant_id, b.id,
       '00000000-0000-4000-8000-000000000102', 'active',
       date_trunc('month', NOW()), date_trunc('month', NOW()) + INTERVAL '1 month'
FROM billing_accounts b
WHERE NOT EXISTS (
    SELECT 1 FROM subscriptions s
    WHERE s.billing_account_id=b.id AND s.status IN ('trialing','active')
);

-- 将套餐权益展开为按周期统计的计数器；JSON 保留套餐定义，权益表保存订阅实际执行的额度状态。
INSERT INTO entitlements (
    id, tenant_id, subscription_id, billing_account_id, entitlement_key,
    limit_value, period_start, period_end
)
SELECT gen_random_uuid(), s.tenant_id, s.id, s.billing_account_id, benefit.key,
       benefit.value::numeric, s.current_period_start, s.current_period_end
FROM subscriptions s
CROSS JOIN LATERAL jsonb_each_text(
    (SELECT pv.benefits FROM plan_versions pv WHERE pv.id=s.plan_version_id)
) benefit
WHERE NOT EXISTS (
    SELECT 1 FROM entitlements e
    WHERE e.subscription_id=s.id AND e.entitlement_key=benefit.key
      AND e.period_start=s.current_period_start
);

INSERT INTO entitlement_usages (id, tenant_id, entitlement_id)
SELECT gen_random_uuid(), e.tenant_id, e.id
FROM entitlements e
WHERE NOT EXISTS (SELECT 1 FROM entitlement_usages u WHERE u.entitlement_id=e.id);

UPDATE installation_settings SET schema_version=GREATEST(schema_version, 3), updated_at=NOW();
