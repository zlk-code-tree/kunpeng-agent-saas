-- 平台管理员与租户角色相互独立；仅当系统不存在平台管理员时，才提升初始默认租户所有者。
ALTER TABLE users
    ADD COLUMN IF NOT EXISTS is_platform_admin BOOLEAN NOT NULL DEFAULT FALSE;

UPDATE users u
SET is_platform_admin=TRUE, updated_at=NOW()
FROM installation_settings i
JOIN tenant_members tm
  ON tm.tenant_id=i.default_tenant_id
 AND tm.role='owner'
 AND tm.status='active'
WHERE u.id=tm.user_id
  AND NOT EXISTS (SELECT 1 FROM users WHERE is_platform_admin=TRUE);

UPDATE installation_settings
SET schema_version=GREATEST(schema_version, 4), updated_at=NOW();
