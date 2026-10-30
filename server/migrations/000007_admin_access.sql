-- 菜单只配置已在前端注册的路由，不能通过数据库注入任意组件或外部链接。
CREATE TABLE IF NOT EXISTS admin_menu_settings (
    route_name VARCHAR(80) PRIMARY KEY,
    title VARCHAR(80) NOT NULL,
    sort_order INTEGER NOT NULL,
    enabled BOOLEAN NOT NULL DEFAULT TRUE,
    protected BOOLEAN NOT NULL DEFAULT FALSE,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

INSERT INTO admin_menu_settings (route_name, title, sort_order, enabled, protected) VALUES
    ('AgentWorkspace', '智能体工作台', -1, TRUE, TRUE),
    ('TenantManagement', '空间与成员', 5, TRUE, TRUE),
    ('Agents', '智能体中心', 10, TRUE, FALSE),
    ('BillingOverview', '套餐与用量', 15, TRUE, FALSE),
    ('KnowledgeBases', '私有知识库', 20, TRUE, FALSE),
    ('SystemOverview', '平台设置', 30, TRUE, TRUE),
    ('EnterpriseOperations', '企业集成与成本', 35, TRUE, FALSE),
    ('MenuManagement', '菜单管理', 40, TRUE, TRUE),
    ('PermissionManagement', '权限管理', 45, TRUE, TRUE)
ON CONFLICT (route_name) DO NOTHING;

UPDATE installation_settings
SET schema_version=GREATEST(schema_version, 7), updated_at=NOW();
