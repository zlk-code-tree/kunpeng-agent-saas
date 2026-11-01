-- 系统管理改为父子菜单后补齐新增的用户管理入口及此前遗漏的角色管理入口。
-- 父级“系统管理”由前端静态注册，数据库只维护可以落到具体页面的叶子菜单。
INSERT INTO admin_menu_settings
    (route_name, title, sort_order, enabled, protected, menu_type, link_url, open_mode, icon)
VALUES
    ('UserManagement', '用户管理', 31, TRUE, TRUE, 'builtin', '', 'window', ''),
    ('RoleManagement', '角色管理', 6, TRUE, FALSE, 'builtin', '', 'window', '')
ON CONFLICT (route_name) DO NOTHING;

UPDATE admin_menu_settings SET sort_order=30 WHERE route_name='SystemOverview';
UPDATE admin_menu_settings SET sort_order=32 WHERE route_name='MenuManagement';
UPDATE admin_menu_settings SET sort_order=33 WHERE route_name='PermissionManagement';

UPDATE installation_settings
SET schema_version=GREATEST(schema_version, 10), updated_at=NOW();
