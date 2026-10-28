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
  createdAt: string;
  id: string;
  name: string;
  status: string;
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

export interface InstallStatus {
  defaultTenantId?: string;
  installed: boolean;
  installationId?: string;
  mode: 'multi' | 'single';
}

export function listAgentsApi() {
  return requestClient.get<AgentItem[]>('/agents');
}

export function listKnowledgeBasesApi() {
  return requestClient.get<KnowledgeBaseItem[]>('/knowledge-bases');
}

export function createKnowledgeBaseApi(data: {
  name: string;
  visibility: 'private' | 'tenant';
}) {
  return requestClient.post<KnowledgeBaseItem>('/knowledge-bases', data);
}

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

export function listKnowledgeDocumentsApi(knowledgeBaseId: string) {
  return requestClient.get<KnowledgeDocumentItem[]>(
    `/knowledge-bases/${knowledgeBaseId}/documents`,
  );
}

export function getCapabilitiesApi() {
  return requestClient.get<CapabilityInfo>('/system/capabilities');
}

export function createRunApi(data: {
  agentVersionId: string;
  conversationId?: string;
  inputs: Record<string, unknown>;
  query: string;
}) {
  return requestClient.post<RunItem>('/runs', data);
}

export function getRunApi(id: string) {
  return requestClient.get<RunItem>(`/runs/${id}`);
}

export function cancelRunApi(id: string) {
  return requestClient.post<{ success: boolean }>(`/runs/${id}/cancel`);
}

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

export function listDifyInstancesApi() {
  return requestClient.get<DifyInstanceItem[]>('/admin/dify-instances');
}

export function createDifyInstanceApi(data: {
  apiKey: string;
  baseUrl: string;
  name: string;
}) {
  return requestClient.post<DifyInstanceItem>('/admin/dify-instances', data);
}

export function bindDifyAppApi(data: {
  agentVersionId: string;
  apiKey: string;
  difyInstanceId: string;
  upstreamAppId?: string;
}) {
  return requestClient.post<{ success: boolean }>('/admin/dify-bindings', data);
}

export function getInstallStatusApi() {
  return requestClient.get<InstallStatus>('/install/status');
}

export function listTenantsApi() {
  return requestClient.get<TenantItem[]>('/tenants');
}

export function createTenantApi(data: { name: string }) {
  return requestClient.post<TenantItem>('/tenants', data);
}

export function switchTenantApi(id: string) {
  return requestClient.post<TenantSwitchResult>(`/tenants/${id}/switch`);
}

export function listTenantMembersApi(id: string) {
  return requestClient.get<TenantMemberItem[]>(`/tenants/${id}/members`);
}

export function inviteTenantMemberApi(
  id: string,
  data: { role: 'admin' | 'finance' | 'member'; username: string },
) {
  return requestClient.post<TenantInvitationResult>(
    `/tenants/${id}/invitations`,
    data,
  );
}

export function acceptTenantInvitationApi(token: string) {
  return requestClient.post<TenantItem>('/tenant-invitations/accept', {
    token,
  });
}

export function removeTenantMemberApi(tenantId: string, userId: string) {
  return requestClient.delete<{ success: boolean }>(
    `/tenants/${tenantId}/members/${userId}`,
  );
}

export function leaveTenantApi(tenantId: string) {
  return requestClient.post<{ success: boolean }>(`/tenants/${tenantId}/leave`);
}

export function transferTenantOwnershipApi(tenantId: string, userId: string) {
  return requestClient.post<{ success: boolean }>(
    `/tenants/${tenantId}/transfer-ownership`,
    { userId },
  );
}

export function installPlatformApi(data: {
  adminName: string;
  adminPassword: string;
  adminUsername: string;
  platformName: string;
  tenantName: string;
}) {
  return requestClient.post<InstallStatus>('/install', data);
}
