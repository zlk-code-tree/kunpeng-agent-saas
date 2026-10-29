-- 首个垂直智能体：Docker 部署助手。模板和版本使用固定 UUID，迁移可重复执行。
INSERT INTO agent_templates (id, name, description, category, status)
VALUES (
    '00000000-0000-4000-8000-000000000201',
    'Docker 部署助手',
    '基于当前 Gin + Vben + PostgreSQL + Redis + 对象存储编排，提供部署、初始化、运维、备份恢复和故障排查指导。',
    '运维',
    'published'
)
ON CONFLICT (id) DO UPDATE SET
    name=EXCLUDED.name,
    description=EXCLUDED.description,
    category=EXCLUDED.category,
    status='published',
    updated_at=NOW();

INSERT INTO agent_versions (
    id, agent_template_id, version, app_type, parameter_schema, status
)
VALUES (
    '00000000-0000-4000-8000-000000000202',
    '00000000-0000-4000-8000-000000000201',
    1,
    'chatbot',
    '{
      "deploymentMode":{"type":"string","title":"部署模式","enum":["development","production"],"default":"production"},
      "appMode":{"type":"string","title":"租户模式","enum":["single","multi"],"default":"single"},
      "managedDify":{"type":"boolean","title":"使用平台托管 Dify","default":true}
    }'::jsonb,
    'published'
)
ON CONFLICT (agent_template_id, version) DO UPDATE SET
    app_type=EXCLUDED.app_type,
    parameter_schema=EXCLUDED.parameter_schema,
    status='published';
