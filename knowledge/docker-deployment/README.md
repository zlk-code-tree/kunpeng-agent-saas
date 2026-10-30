# Docker 部署助手知识库

## 知识库范围

本目录从当前仓库中的真实部署配置提取，供“Docker 部署助手”检索：

| 文档 | 主要来源 |
|---|---|
| `00-项目部署架构.md` | `deploy/compose.yaml`、`deploy/compose.prod.yaml` |
| `01-部署前检查.md` | Compose 端口、配置校验规则 |
| `02-开发环境部署.md` | `Makefile`、`server/.env.example` |
| `03-生产环境部署.md` | 生产 Compose、Dockerfile、Nginx 配置 |
| `04-配置项说明.md` | `server/internal/config`、对象存储实现 |
| `05-Dify与Docker部署助手配置.md` | Dify 集成、知识库和智能体绑定实现 |
| `06-日常运维与升级.md` | 健康检查、模式迁移和 Compose 操作 |
| `07-备份与恢复.md` | `deploy/scripts/backup.sh`、`restore.sh` |
| `08-故障排查.md` | 当前项目启动约束和常见失败路径 |

## 批量导入平台知识库

平台 API、Worker 和 Dify 实例配置完成后执行：

```bash
PLATFORM_API_BASE_URL=http://127.0.0.1/api/v1 \
PLATFORM_ACCESS_TOKEN='替换为登录访问令牌' \
KNOWLEDGE_VISIBILITY=tenant \
./deploy/scripts/import-deployment-knowledge.sh
```

如果 API 直接运行在开发端口：

```bash
PLATFORM_API_BASE_URL=http://127.0.0.1:8080/api/v1 \
PLATFORM_ACCESS_TOKEN='替换为登录访问令牌' \
KNOWLEDGE_VISIBILITY=private \
./deploy/scripts/import-deployment-knowledge.sh
```

脚本只调用公开 API，不直接修改 PostgreSQL 或对象存储。返回 `indexing` 表示已提交 Dify；返回 `waiting` 表示文件已保存在本地，但 Dify 尚未完成配置或提交失败。

## 智能体配置

- 产品清单：`agent-packages/docker-deployment/manifest.yaml`
- 系统提示词：`agent-packages/docker-deployment/prompts/system.md`
- 数据库种子：`server/migrations/000006_docker_deployment_agent.sql`

智能体只有在绑定有效 Dify Chatbot API Key 后才可以运行。
