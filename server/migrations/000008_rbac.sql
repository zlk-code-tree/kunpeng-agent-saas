-- 统一 RBAC：权限码同时驱动前端按钮显隐、接口放行与菜单可见性。
-- scope 隔离铁律：平台角色只能绑定平台权限码，租户角色只能绑定业务权限码。
CREATE TABLE IF NOT EXISTS permissions (
    code VARCHAR(64) PRIMARY KEY,
    name VARCHAR(80) NOT NULL,
    scope VARCHAR(16) NOT NULL CHECK (scope IN ('platform', 'tenant')),
    group_name VARCHAR(64) NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    sort_order INTEGER NOT NULL DEFAULT 0
);

-- builtin=TRUE 的内置角色不可删除；tenant_id 为空表示平台角色或全租户共享的内置租户角色。
CREATE TABLE IF NOT EXISTS roles (
    id UUID PRIMARY KEY,
    code VARCHAR(64) NOT NULL,
    name VARCHAR(80) NOT NULL,
    scope VARCHAR(16) NOT NULL CHECK (scope IN ('platform', 'tenant')),
    builtin BOOLEAN NOT NULL DEFAULT FALSE,
    tenant_id UUID REFERENCES tenants(id),
    description TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE UNIQUE INDEX IF NOT EXISTS uq_roles_builtin_code ON roles (code) WHERE tenant_id IS NULL;
CREATE UNIQUE INDEX IF NOT EXISTS uq_roles_tenant_code ON roles (tenant_id, code) WHERE tenant_id IS NOT NULL;

CREATE TABLE IF NOT EXISTS role_permissions (
    role_id UUID NOT NULL REFERENCES roles(id) ON DELETE CASCADE,
    permission_code VARCHAR(64) NOT NULL REFERENCES permissions(code),
    PRIMARY KEY (role_id, permission_code)
);

-- 安装级用户与平台角色的绑定；与 users.is_platform_admin 同步维护。
CREATE TABLE IF NOT EXISTS user_roles (
    user_id UUID NOT NULL REFERENCES users(id),
    role_id UUID NOT NULL REFERENCES roles(id) ON DELETE CASCADE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (user_id, role_id)
);

-- 租户成员与租户角色的绑定（含内置角色与自定义角色），与 tenant_members.role 同步维护。
CREATE TABLE IF NOT EXISTS member_roles (
    tenant_id UUID NOT NULL REFERENCES tenants(id),
    user_id UUID NOT NULL REFERENCES users(id),
    role_id UUID NOT NULL REFERENCES roles(id) ON DELETE CASCADE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (tenant_id, user_id, role_id)
);

-- 邀请可携带自定义角色；内置角色仍由 role 列表达。
ALTER TABLE tenant_invitations
    ADD COLUMN IF NOT EXISTS role_id UUID REFERENCES roles(id);

INSERT INTO permissions (code, name, scope, group_name, description, sort_order) VALUES
    ('agent:view',        '查看智能体',     'tenant',   '智能体',   '浏览智能体列表与详情', 10),
    ('agent:run',         '运行智能体',     'tenant',   '智能体',   '发起智能体运行', 20),
    ('agent:manage',      '配置智能体',     'tenant',   '智能体',   '编辑智能体配置与参数', 30),
    ('agent:delete',      '删除智能体',     'tenant',   '智能体',   '删除智能体及其配置', 40),
    ('knowledge:view',    '查看知识库',     'tenant',   '知识库',   '浏览知识库与文档列表', 50),
    ('knowledge:create',  '新建知识库',     'tenant',   '知识库',   '创建知识库', 60),
    ('knowledge:upload',  '上传文档',       'tenant',   '知识库',   '向知识库上传文档', 70),
    ('knowledge:delete',  '删除知识库',     'tenant',   '知识库',   '删除知识库或文档', 80),
    ('run:view',          '查看运行',       'tenant',   '运行',     '查看运行记录与事件', 90),
    ('run:cancel',        '取消运行',       'tenant',   '运行',     '取消进行中的运行', 100),
    ('run:tool-ticket',   '申请工具票据',   'tenant',   '运行',     '为运行申请工具调用票据', 110),
    ('billing:view',      '查看套餐',       'tenant',   '账单',     '查看套餐与账户概览', 120),
    ('billing:subscribe', '订阅套餐',       'tenant',   '账单',     '订阅、购买或变更套餐', 130),
    ('billing:ledger-view', '查看账本',     'tenant',   '账单',     '查看账本明细', 140),
    ('member:view',       '查看成员',       'tenant',   '成员',     '查看空间成员列表', 150),
    ('member:invite',     '邀请成员',       'tenant',   '成员',     '邀请新成员加入空间', 160),
    ('member:remove',     '移除成员',       'tenant',   '成员',     '从空间移除成员', 170),
    ('member:role-assign', '分配成员角色',  'tenant',   '成员',     '为成员分配角色', 180),
    ('role:manage',       '管理角色',       'tenant',   '角色',     '创建与维护自定义角色及其权限码', 190),
    ('tenant:settings',   '空间设置',       'tenant',   '空间',     '管理空间设置与创建空间', 200),
    ('tenant:switch',     '切换空间',       'tenant',   '空间',     '在已加入的空间之间切换', 210),
    ('instance:manage',   'Dify 实例管理',  'platform', '平台集成', '管理 Dify 实例与凭据', 310),
    ('provider:manage',   '模型服务商管理', 'platform', '平台集成', '管理模型服务商', 320),
    ('route:manage',      '模型路由管理',   'platform', '平台集成', '管理模型路由', 330),
    ('price:manage',      '模型价格管理',   'platform', '平台集成', '管理模型价格', 340),
    ('tool:manage',       '工具管理',       'platform', '平台集成', '管理工具定义', 350),
    ('grant:manage',      '工具授权管理',   'platform', '平台集成', '管理工具授权', 360),
    ('billing:adjust',    '余额调整',       'platform', '平台计费', '调整计费账户余额', 370),
    ('cost:view',         '成本总览',       'platform', '平台计费', '查看成本汇总', 380),
    ('reconcile',         '对账',           'platform', '平台计费', '执行对账任务', 390),
    ('tenant:export',     '租户数据导出',   'platform', '平台系统', '导出租户数据', 400),
    ('menu:manage',       '菜单管理',       'platform', '平台系统', '管理菜单显示配置', 410),
    ('user:manage',       '用户管理',       'platform', '平台系统', '管理平台账号', 420),
    ('user:platform-admin', '授予平台管理员', 'platform', '平台系统', '授予或撤销平台管理员', 430)
ON CONFLICT (code) DO NOTHING;

-- 内置角色。owner/admin/member/finance 的 code 与 tenant_members.role 取值保持一致。
INSERT INTO roles (id, code, name, scope, builtin, tenant_id, description) VALUES
    (gen_random_uuid(), 'super_admin', '超级管理员', 'platform', TRUE, NULL, '安装级平台管理员，旁路全部平台权限码'),
    (gen_random_uuid(), 'owner',       '所有者',     'tenant',   TRUE, NULL, '空间所有者，旁路本空间全部权限码'),
    (gen_random_uuid(), 'admin',       '管理员',     'tenant',   TRUE, NULL, '空间管理员，除订阅支付外的全部业务权限'),
    (gen_random_uuid(), 'member',      '成员',       'tenant',   TRUE, NULL, '普通成员，智能体、知识库与运行的日常使用'),
    (gen_random_uuid(), 'finance',     '财务',       'tenant',   TRUE, NULL, '财务成员，套餐订阅与账本查看')
ON CONFLICT DO NOTHING;

-- 角色-权限码绑定：平台角色只绑平台码，租户角色只绑业务码。
INSERT INTO role_permissions (role_id, permission_code)
SELECT r.id, p.code
FROM roles r
JOIN permissions p ON p.scope = 'platform'
WHERE r.code = 'super_admin' AND r.tenant_id IS NULL
ON CONFLICT DO NOTHING;

INSERT INTO role_permissions (role_id, permission_code)
SELECT r.id, p.code
FROM roles r
JOIN permissions p ON p.scope = 'tenant'
WHERE r.code = 'owner' AND r.tenant_id IS NULL
ON CONFLICT DO NOTHING;

INSERT INTO role_permissions (role_id, permission_code)
SELECT r.id, p.code
FROM roles r
JOIN permissions p ON p.scope = 'tenant' AND p.code <> 'billing:subscribe'
WHERE r.code = 'admin' AND r.tenant_id IS NULL
ON CONFLICT DO NOTHING;

INSERT INTO role_permissions (role_id, permission_code)
SELECT r.id, p.code
FROM roles r
JOIN permissions p ON p.scope = 'tenant'
WHERE r.code = 'member' AND r.tenant_id IS NULL
  AND p.code IN (
    'agent:view', 'agent:run',
    'knowledge:view', 'knowledge:create', 'knowledge:upload', 'knowledge:delete',
    'run:view', 'run:cancel', 'run:tool-ticket',
    'member:view', 'tenant:switch'
  )
ON CONFLICT DO NOTHING;

INSERT INTO role_permissions (role_id, permission_code)
SELECT r.id, p.code
FROM roles r
JOIN permissions p ON p.scope = 'tenant'
WHERE r.code = 'finance' AND r.tenant_id IS NULL
  AND p.code IN (
    'agent:view', 'run:view',
    'billing:view', 'billing:subscribe', 'billing:ledger-view',
    'member:view', 'tenant:switch'
  )
ON CONFLICT DO NOTHING;

-- 回填：平台管理员绑定 super_admin。
INSERT INTO user_roles (user_id, role_id)
SELECT u.id, r.id
FROM users u
JOIN roles r ON r.code = 'super_admin' AND r.scope = 'platform' AND r.tenant_id IS NULL
WHERE u.is_platform_admin
ON CONFLICT DO NOTHING;

-- 回填：现有成员按 tenant_members.role 绑定内置租户角色。
INSERT INTO member_roles (tenant_id, user_id, role_id)
SELECT tm.tenant_id, tm.user_id, r.id
FROM tenant_members tm
JOIN roles r ON r.code = tm.role AND r.scope = 'tenant' AND r.tenant_id IS NULL
ON CONFLICT DO NOTHING;

UPDATE installation_settings
SET schema_version=GREATEST(schema_version, 8), updated_at=NOW();
