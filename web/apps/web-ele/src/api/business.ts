import { requestClient } from '#/api/request';

export interface AgentItem {
  agentVersionId: string;
  category: string;
  configured: boolean;
  description: string;
  enabled: boolean;
  id: string;
  name: string;
  parameterSchema: Record<string, unknown>;
  version: number;
}

export interface RunItem {
  conversationId?: string;
  errorMessage?: string;
  id: string;
  output?: { answer?: string; outputs?: Record<string, unknown> };
  status: string;
}

export interface RunEventItem {
  createdAt: string;
  eventType: string;
  payload: Record<string, unknown>;
  sequence: number;
}

export interface KnowledgeBaseItem {
  createdAt: string;
  id: string;
  name: string;
  status: string;
  visibility: 'private' | 'tenant';
}

export interface KnowledgeDocumentItem {
  contentType: string;
  createdAt: string;
  id: string;
  indexingStatus: string;
  name: string;
  sizeBytes: number;
}

export interface CapabilityInfo {
  appMode: 'multi' | 'single';
  features: Record<string, boolean>;
}

export interface DifyInstanceItem {
  baseUrl: string;
  consecutiveFailures: number;
  credentialVersion: number;
  createdAt: string;
  deploymentType: 'dedicated' | 'shared';
  healthStatus: 'degraded' | 'healthy' | 'unavailable' | 'unknown';
  id: string;
  lastHealthCheckAt?: string;
  name: string;
  status: string;
}

export interface ModelProviderItem {
  baseUrl: string;
  code: string;
  createdAt: string;
  healthStatus: string;
  id: string;
  lastHealthAt?: string;
  name: string;
  providerType: string;
  shared: boolean;
  status: string;
}

export interface ToolItem {
  code: string;
  createdAt: string;
  description: string;
  endpoint: string;
  id: string;
  name: string;
  protocol: 'http_json' | 'mcp_http';
  status: string;
  version: number;
}

export interface CostSummaryItem {
  amount: string;
  costType: string;
  currency: string;
  model: string;
  provider: string;
}

export interface ReconciliationReport {
  reservationsAwaitingUsage: number;
  reservationsInReview: number;
  succeededRunsMissingUsage: number;
  usageMissingCost: number;
}

export interface TenantItem {
  createdAt: string;
  current: boolean;
  id: string;
  name: string;
  role: 'admin' | 'finance' | 'member' | 'owner';
  status: string;
  type: 'organization' | 'personal';
}

export interface TenantMemberItem {
  displayName: string;
  joinedAt: string;
  role: 'admin' | 'finance' | 'member' | 'owner';
  status: string;
  userId: string;
  username: string;
}

export interface TenantSwitchResult {
  accessToken: string;
  role: string;
  tenantId: string;
}

export interface TenantInvitationResult {
  expiresAt: string;
  id: string;
  role: string;
  tenantId: string;
  token: string;
  username: string;
}

export interface BillingAccount {
  available: string;
  balance: string;
  currency: string;
  frozenAmount: string;
  id: string;
  status: string;
  subjectType: 'tenant' | 'user';
}

export interface BillingPlan {
  audience: 'all' | 'organization' | 'personal';
  benefits: Record<string, number>;
  billingPeriod: 'month' | 'one_time' | 'year';
  code: string;
  currency: string;
  id: string;
  name: string;
  price: string;
  version: number;
  versionId: string;
}

export interface BillingEntitlement {
  key: string;
  limit: string;
  periodEnd: string;
  reserved: string;
  used: string;
}

export interface BillingSubscription {
  cancelAtPeriodEnd: boolean;
  currentPeriodEnd: string;
  currentPeriodStart: string;
  entitlements: BillingEntitlement[];
  id: string;
  planCode: string;
  planName: string;
  status: string;
}

export interface BillingLedgerEntry {
  amount: string;
  balanceAfter: string;
  businessId?: string;
  businessType: string;
  createdAt: string;
  entryType: string;
  frozenAfter: string;
  id: string;
  metadata: Record<string, unknown>;
}

export interface InstallStatus {
  defaultTenantId?: string;
  installed: boolean;
  installationId?: string;
  mode: 'multi' | 'single';
}

/** 获取当前租户可使用的智能体列表。 */
export function listAgentsApi() {
  return requestClient.get<AgentItem[]>('/agents');
}

/** 获取当前用户可见的知识库列表。 */
export function listKnowledgeBasesApi() {
  return requestClient.get<KnowledgeBaseItem[]>('/knowledge-bases');
}

/** 创建私有或租户共享知识库。 */
export function createKnowledgeBaseApi(data: {
  name: string;
  visibility: 'private' | 'tenant';
}) {
  return requestClient.post<KnowledgeBaseItem>('/knowledge-bases', data);
}

/** 上传知识库文档，并为较大文件设置独立超时时间。 */
export function uploadKnowledgeDocumentApi(
  knowledgeBaseId: string,
  file: File,
) {
  return requestClient.upload<KnowledgeDocumentItem>(
    `/knowledge-bases/${knowledgeBaseId}/documents`,
    { file },
    { timeout: 120_000 },
  );
}

/** 获取指定知识库的文档和索引状态。 */
export function listKnowledgeDocumentsApi(knowledgeBaseId: string) {
  return requestClient.get<KnowledgeDocumentItem[]>(
    `/knowledge-bases/${knowledgeBaseId}/documents`,
  );
}

/** 获取当前安装模式支持的平台能力。 */
export function getCapabilitiesApi() {
  return requestClient.get<CapabilityInfo>('/system/capabilities');
}

/** 获取当前计费主体的钱包余额。 */
export function getBillingAccountApi() {
  return requestClient.get<BillingAccount>('/billing/account');
}

/** 获取当前租户可用的套餐。 */
export function listBillingPlansApi() {
  return requestClient.get<BillingPlan[]>('/billing/plans');
}

/** 获取当前订阅和权益使用量。 */
export function getBillingSubscriptionApi() {
  return requestClient.get<BillingSubscription>('/billing/subscription');
}

/** 获取最近的钱包流水。 */
export function listBillingLedgerApi(limit = 50) {
  return requestClient.get<BillingLedgerEntry[]>('/billing/ledger', {
    params: { limit },
  });
}

/** 创建一次异步智能体运行。 */
export function createRunApi(data: {
  agentVersionId: string;
  conversationId?: string;
  inputs: Record<string, unknown>;
  query: string;
}) {
  return requestClient.post<RunItem>('/runs', data);
}

/** 查询一次运行的最新状态和输出。 */
export function getRunApi(id: string) {
  return requestClient.get<RunItem>(`/runs/${id}`);
}

/** 请求取消正在排队或执行的运行。 */
export function cancelRunApi(id: string) {
  return requestClient.post<{ success: boolean }>(`/runs/${id}/cancel`);
}

/** 建立运行 SSE 事件流，并允许调用方传入续传请求头和取消信号。 */
export function streamRunEventsApi(
  id: string,
  options: {
    headers?: HeadersInit;
    onEnd?: () => void;
    onMessage?: (message: string) => void;
    signal?: AbortSignal;
  },
) {
  return requestClient.requestSSE(`/runs/${id}/events`, undefined, options);
}

/** 获取平台管理员可见的 Dify 实例。 */
export function listDifyInstancesApi() {
  return requestClient.get<DifyInstanceItem[]>('/admin/dify-instances');
}

/** 创建共享或租户独享的 Dify 实例。 */
export function createDifyInstanceApi(data: {
  apiKey: string;
  baseUrl: string;
  deploymentType?: 'dedicated' | 'shared';
  name: string;
}) {
  return requestClient.post<DifyInstanceItem>('/admin/dify-instances', data);
}

/** 立即执行一次 Dify 实例健康检查。 */
export function checkDifyInstanceApi(id: string) {
  return requestClient.post<DifyInstanceItem>(
    `/admin/dify-instances/${id}/health-check`,
  );
}

/** 获取当前租户可使用的模型供应商。 */
export function listModelProvidersApi() {
  return requestClient.get<ModelProviderItem[]>('/admin/model-providers');
}

/** 创建模型供应商配置。 */
export function createModelProviderApi(data: {
  apiKey?: string;
  baseUrl: string;
  code: string;
  name: string;
  providerType: string;
  shared?: boolean;
}) {
  return requestClient.post<ModelProviderItem>('/admin/model-providers', data);
}

/** 发布指定供应商模型的新成本价格版本。 */
export function setModelPriceApi(
  id: string,
  data: {
    currency?: string;
    inputPer1000: string;
    model: string;
    outputPer1000: string;
  },
) {
  return requestClient.post<{ success: boolean }>(
    `/admin/model-providers/${id}/prices`,
    data,
  );
}

/** 获取受控工具目录。 */
export function listToolsApi() {
  return requestClient.get<ToolItem[]>('/admin/tools');
}

/** 创建 HTTP 或 MCP 工具及其首个版本。 */
export function createToolApi(data: {
  code: string;
  credential?: string;
  description?: string;
  endpoint: string;
  name: string;
  protocol: 'http_json' | 'mcp_http';
}) {
  return requestClient.post<ToolItem>('/admin/tools', data);
}

/** 为租户授权工具并设置智能体白名单和调用上限。 */
export function grantToolApi(
  id: string,
  data: {
    allowedAgentVersionIds?: string[];
    maxCallsPerRun?: number;
    tenantId: string;
  },
) {
  return requestClient.post<{ success: boolean }>(
    `/admin/tools/${id}/grants`,
    data,
  );
}

/** 获取最近一个月的供应商成本汇总。 */
export function getCostSummaryApi() {
  return requestClient.get<CostSummaryItem[]>('/admin/costs/summary');
}

/** 执行一次只读账务对账扫描。 */
export function reconcileBillingApi() {
  return requestClient.post<ReconciliationReport>('/admin/reconciliation');
}

/** 导出指定租户的一致性业务快照。 */
export function exportTenantApi(id: string) {
  return requestClient.post<{
    checksumSha256: string;
    exportId: string;
    exportedAt: string;
    manifest: Record<string, unknown>;
  }>(`/admin/tenants/${id}/export`);
}

/** 将智能体版本绑定到 Dify 应用。 */
export function bindDifyAppApi(data: {
  agentVersionId: string;
  apiKey: string;
  difyInstanceId: string;
  upstreamAppId?: string;
}) {
  return requestClient.post<{ success: boolean }>('/admin/dify-bindings', data);
}

/** 获取平台安装状态。 */
export function getInstallStatusApi() {
  return requestClient.get<InstallStatus>('/install/status');
}

/** 获取当前用户加入的租户列表。 */
export function listTenantsApi() {
  return requestClient.get<TenantItem[]>('/tenants');
}

/** 创建企业租户空间。 */
export function createTenantApi(data: { name: string }) {
  return requestClient.post<TenantItem>('/tenants', data);
}

/** 切换租户并获取绑定目标租户的新访问令牌。 */
export function switchTenantApi(id: string) {
  return requestClient.post<TenantSwitchResult>(`/tenants/${id}/switch`);
}

/** 获取指定租户的有效成员。 */
export function listTenantMembersApi(id: string) {
  return requestClient.get<TenantMemberItem[]>(`/tenants/${id}/members`);
}

/** 邀请已注册用户加入租户。 */
export function inviteTenantMemberApi(
  id: string,
  data: { role: 'admin' | 'finance' | 'member'; username: string },
) {
  return requestClient.post<TenantInvitationResult>(
    `/tenants/${id}/invitations`,
    data,
  );
}

/** 使用一次性令牌接受租户邀请。 */
export function acceptTenantInvitationApi(token: string) {
  return requestClient.post<TenantItem>('/tenant-invitations/accept', {
    token,
  });
}

/** 从租户移除指定成员。 */
export function removeTenantMemberApi(tenantId: string, userId: string) {
  return requestClient.delete<{ success: boolean }>(
    `/tenants/${tenantId}/members/${userId}`,
  );
}

/** 当前用户退出指定租户。 */
export function leaveTenantApi(tenantId: string) {
  return requestClient.post<{ success: boolean }>(`/tenants/${tenantId}/leave`);
}

/** 将租户所有权转移给指定成员。 */
export function transferTenantOwnershipApi(tenantId: string, userId: string) {
  return requestClient.post<{ success: boolean }>(
    `/tenants/${tenantId}/transfer-ownership`,
    { userId },
  );
}

/** 提交平台首次安装配置。 */
export function installPlatformApi(data: {
  adminName: string;
  adminPassword: string;
  adminUsername: string;
  platformName: string;
  tenantName: string;
}) {
  return requestClient.post<InstallStatus>('/install', data);
}
