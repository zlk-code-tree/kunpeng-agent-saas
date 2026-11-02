-- “系统管理”作为统一后台入口，角色管理迁入该分组后调整子菜单顺序。
-- 权限边界仍由前端子路由 authority 与后端接口 RequirePermission 双重保证。
UPDATE admin_menu_settings SET sort_order=32 WHERE route_name='RoleManagement';
UPDATE admin_menu_settings SET sort_order=33 WHERE route_name='MenuManagement';
UPDATE admin_menu_settings SET sort_order=34 WHERE route_name='PermissionManagement';

UPDATE installation_settings
SET schema_version=GREATEST(schema_version, 11), updated_at=NOW();
