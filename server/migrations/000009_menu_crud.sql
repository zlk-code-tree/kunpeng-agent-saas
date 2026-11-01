-- 菜单增删改查：内置路由行只允许编辑；自定义菜单行（menu_type='link'）支持新增与删除，
-- 通过外链（新窗口）或内嵌 iframe 打开，不向数据库开放任意组件注入。
ALTER TABLE admin_menu_settings
    ADD COLUMN IF NOT EXISTS menu_type VARCHAR(16) NOT NULL DEFAULT 'builtin',
    ADD COLUMN IF NOT EXISTS link_url TEXT NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS open_mode VARCHAR(16) NOT NULL DEFAULT 'window',
    ADD COLUMN IF NOT EXISTS icon VARCHAR(80) NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS created_at TIMESTAMPTZ NOT NULL DEFAULT NOW();

UPDATE installation_settings
SET schema_version=GREATEST(schema_version, 9), updated_at=NOW();
