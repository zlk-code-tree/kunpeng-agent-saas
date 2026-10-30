# Dify 与 Docker 部署助手配置

## 前置状态

Docker 部署助手由两部分组成：

1. Gin SaaS 平台中的智能体模板和版本。
2. Dify 中实际执行问答的 Chatbot 应用及知识库。

只创建平台模板但没有 Dify 应用绑定时，智能体中心会显示“等待绑定 Dify”，不能发起运行。

## 平台预置标识

```text
智能体模板 ID：00000000-0000-4000-8000-000000000201
智能体版本 ID：00000000-0000-4000-8000-000000000202
应用类型：chatbot
```

这些标识由 `server/migrations/000006_docker_deployment_agent.sql` 幂等创建。

## 通过 DSL 导入应用（推荐）

仓库中已提供可直接导入的 DSL 文件：

```text
agent-packages/docker-deployment/dify/docker-deployment-assistant.yml
```

导入步骤：

1. 先在 Dify 中创建知识库“Gin Dify SaaS 部署文档”，上传 `knowledge/docker-deployment/` 下全部 Markdown 文档。
2. 打开 Dify 工作室，点击“导入 DSL 文件”，选择上述文件。
3. 进入应用编排页，点击“检索部署知识库”节点，在知识库下拉框中勾选步骤 1 创建的知识库（DSL 中 `dataset_ids` 为空，必须手动绑定）。
4. 点击“生成部署指导”LLM 节点，确认模型为当前工作区已配置的 DeepSeek `deepseek-flash`；若其他实例没有该模型，选择该实例中已配置的聊天模型。
5. 保存并发布，使用文末“验收问题”逐一验证。

DSL 已预置开场白、5 个建议问题和系统提示词；模型参数为 temperature 0.2、max_tokens 2048、记忆窗口 10 轮；DSL 版本 0.7.0，与 Dify 新版导出格式一致。

## 创建 Dify Chatbot

在目标 Dify 中创建 Chatbot 应用，名称建议为“Docker 部署助手”。系统提示词使用：

```text
agent-packages/docker-deployment/prompts/system.md
```

应用应启用引用知识库回答，并设置合适的召回策略。不要把真实密码、API Key 或客户敏感配置放进系统提示词。

## 创建知识库

知识文档目录：

```text
knowledge/docker-deployment/
```

可以通过平台“私有知识库”页面创建“Gin Dify SaaS 部署文档”，然后逐个上传 Markdown 文件。若平台已经配置可用的 Dify 实例，创建知识库时会同步创建 Dify Dataset，上传文档时会提交索引。

也可以使用批量脚本：

```bash
PLATFORM_API_BASE_URL=http://127.0.0.1/api/v1 \
PLATFORM_ACCESS_TOKEN='登录后获得的访问令牌' \
KNOWLEDGE_VISIBILITY=tenant \
./deploy/scripts/import-deployment-knowledge.sh
```

脚本返回知识库 ID，并逐个输出文档上传结果。访问令牌只通过环境变量传入，不要写入脚本。

## 注册 Dify 实例

平台管理员在“企业控制台”中添加 Dify 实例：

- 名称：平台托管 Dify 或客户私有 Dify。
- Base URL：Worker 可访问的 Dify API `/v1` 基址。
- API Key：可管理 Dataset 的 Dify 知识库 API 凭据。
- 类型：共享实例或租户独享实例。

注册后执行健康检查。健康检查会访问 `{BaseURL}/info`。网络错误和 5xx 会把实例标记为不可用，4xx 表示服务可达但凭据配置异常。

## 绑定 Dify 应用

从 Dify Chatbot 的 API Access 页面取得应用 API Key，在平台执行“绑定 Dify 应用”：

```text
agentVersionId = 00000000-0000-4000-8000-000000000202
difyInstanceId = 已注册的 Dify 实例 ID
apiKey = Chatbot 应用 API Key
upstreamAppId = Dify 应用 ID（用于审计，可选）
```

知识库管理凭据和 Chatbot 应用 API Key 用途不同，不应混用。

## 知识库与应用关联

平台上传文档后，还需要确认 Dify Chatbot 的知识检索节点或上下文已经关联对应 Dataset。仅仅上传 Dataset 不代表 Chatbot 会自动检索它。

## 验收问题

绑定完成后建议依次验证：

1. “当前 Docker 环境只启动了哪些组件？”
2. “完整生产部署命令是什么？”
3. “为什么容器内不能连接 127.0.0.1:15432？”
4. “如何安全备份和恢复？”
5. “平台托管 Dify 时客户侧需要部署哪些组件？”

回答应引用项目实际 Compose 结构，并在破坏性操作前提示风险。
