/**
 * Query API client.
 *
 * 鉴权: Bearer token (从 localStorage 读取, 由 TokenGate 写入).
 * 基地址优先级:
 *   1. 运行时 window.__LLMLOG_CONFIG__.apiBaseUrl (容器 entrypoint 注入)
 *   2. 编译时 import.meta.env.VITE_API_BASE_URL
 *   3. 回退 '' (相对路径, 走 Vite 开发代理 / Nginx 同域反代)
 */

const TOKEN_KEY = 'llm_logger_api_token'

declare global {
  interface Window {
    __LLMLOG_CONFIG__?: { apiBaseUrl?: string }
  }
}

function resolveApiBase(): string {
  if (typeof window !== 'undefined' && window.__LLMLOG_CONFIG__?.apiBaseUrl) {
    const v = window.__LLMLOG_CONFIG__.apiBaseUrl
    return v === '/api' ? '' : v.replace(/\/api\/?$/, '')
  }
  const env = (import.meta as unknown as { env?: Record<string, string> }).env
  if (env?.VITE_API_BASE_URL) {
    return env.VITE_API_BASE_URL.replace(/\/api\/?$/, '')
  }
  return ''
}

const API_BASE = resolveApiBase()

export function getToken(): string {
  return localStorage.getItem(TOKEN_KEY) || ''
}
export function setToken(token: string) {
  localStorage.setItem(TOKEN_KEY, token)
}
export function clearToken() {
  localStorage.removeItem(TOKEN_KEY)
}

class HTTPError extends Error {
  status: number
  constructor(status: number, message: string) {
    super(message)
    this.status = status
  }
}

interface RequestOptions extends RequestInit {
  /** 跳过 401 自动 reload (用于 TokenGate 验证场景) */
  _noReloadOn401?: boolean
}

async function request<T>(path: string, init?: RequestOptions): Promise<T> {
  const token = getToken()
  const headers: Record<string, string> = {
    'Content-Type': 'application/json',
    ...(init?.headers as Record<string, string> | undefined),
  }
  if (token) headers.Authorization = `Bearer ${token}`

  const url = API_BASE + path
  const { _noReloadOn401, ...fetchInit } = init || {}
  const resp = await fetch(url, { ...fetchInit, headers })
  if (resp.status === 401 || resp.status === 403) {
    clearToken()
    if (!_noReloadOn401) {
      // 强制回到 TokenGate 登录页 (App.tsx 的 authed state 不会自动更新)
      window.location.reload()
    }
    throw new HTTPError(resp.status, '鉴权失败,请重新输入 Token')
  }
  if (!resp.ok) {
    let detail = `HTTP ${resp.status}`
    try {
      const j = await resp.json()
      detail = j.detail || detail
    } catch {
      /* ignore */
    }
    throw new HTTPError(resp.status, detail)
  }
  return resp.json() as Promise<T>
}

// ============ 类型 (与 query-api/app/schemas 对齐) ============

export interface TraceSummary {
  request_id: string
  created_at: string
  provider: string
  endpoint: string
  model: string
  is_stream: boolean
  user_id?: string
  api_key_prefix?: string
  client_type?: string
  client_app_version?: string
  upstream_status: number
  is_error: boolean
  error_message?: string
  duration_ms: number
  ttft_ms: number
  prompt_tokens: number
  completion_tokens: number
  total_tokens: number
  request_text_preview?: string
  response_text_preview?: string
}

export interface TraceDetail extends TraceSummary {
  parent_request_id?: string
  completed_at?: string
  client_ip?: string
  client_user_agent?: string
  api_key_hash?: string
  organization_id?: string
  http_method: string
  model_normalized?: string
  request_message_count?: number
  request_has_image?: boolean
  request_has_audio?: boolean
  request_has_video?: boolean
  request_has_tool?: boolean
  request_temperature?: number | null
  request_top_p?: number | null
  request_max_tokens?: number | null
  response_finish_reason?: string
  response_chunk_count?: number
  cached_tokens?: number
  reasoning_tokens?: number
  error_type?: string
  request_size_bytes?: number
  response_size_bytes?: number
  request_json?: string | null
  response_json?: string | null
  tool_calls_json?: string | null
  request_cos_key?: string
  response_cos_key?: string
}

export interface TraceListResponse {
  items: TraceSummary[]
  total: number
  limit: number
  offset: number
}

export interface UsageRow {
  bucket: string
  provider: string
  model: string
  request_count: number
  error_count: number
  prompt_tokens: number
  completion_tokens: number
  total_tokens: number
  cached_tokens?: number
  reasoning_tokens?: number
  avg_latency_ms: number
  request_bytes?: number
  response_bytes?: number
}

export interface UsageStatsResponse {
  items: UsageRow[]
  group_by: string
  start: string
  end: string
}

export interface ProviderHealth {
  provider: string
  requests: number
  errors: number
  error_rate_pct: number
  avg_latency_ms: number
  avg_ttft_ms?: number | null
  tokens: number
}

export interface HealthStatsResponse {
  items: ProviderHealth[]
  window: string
}

export interface SearchHit {
  request_id: string
  created_at: string
  provider: string
  model: string
  user_id?: string
  client_type?: string
  snippet: string
  is_error: boolean
}

export interface ErrorRow {
  request_id: string
  created_at: string
  provider: string
  model: string
  user_id?: string
  client_type?: string
  upstream_status: number
  error_type: string
  error_message: string
  duration_ms: number
  request_text_preview: string
}

export interface UserProfile {
  user_id: string
  client_type?: string
  api_key_prefix?: string
  user_agent?: string
  total_requests: number
  total_errors: number
  prompt_tokens: number
  completion_tokens: number
  total_tokens: number
  cached_tokens?: number
  reasoning_tokens?: number
  request_bytes?: number
  response_bytes?: number
  models_used: string[]
  providers_used: string[]
  first_seen: string
  last_seen: string
  avg_duration_ms: number
}

export interface UserListResponse {
  items: UserProfile[]
  total: number
}

export interface ErrorListResponse {
  items: ErrorRow[]
  total: number
}

// ============ 端点 ============

export interface ListTracesParams {
  provider?: string
  model?: string
  user_id?: string
  api_key_prefix?: string
  client_type?: string
  is_error?: boolean
  start?: string
  end?: string
  limit?: number
  offset?: number
}

export interface ExportTracesParams {
  format?: 'jsonl' | 'csv'
  include_body?: boolean
  provider?: string
  client_type?: string
  user_id?: string
  api_key_prefix?: string
  is_error?: boolean
  start?: string
  end?: string
  max_rows?: number
}

/** 把任意对象转为 URLSearchParams, 跳过 undefined / null / 空字符串 */
function toQuery(params: object): URLSearchParams {
  const q = new URLSearchParams()
  for (const [k, v] of Object.entries(params)) {
    if (v === undefined || v === null) continue
    const s = String(v)
    if (s === '') continue
    q.set(k, s)
  }
  return q
}

export const api = {
  async listTraces(params: ListTracesParams = {}): Promise<TraceListResponse> {
    return request<TraceListResponse>(`/api/v1/traces?${toQuery(params).toString()}`)
  },

  async getTrace(requestId: string, includeRaw = false): Promise<TraceDetail> {
    const q = new URLSearchParams()
    if (includeRaw) q.set('include_raw', 'true')
    return request<TraceDetail>(`/api/v1/traces/${requestId}?${q.toString()}`)
  },

  async usage(params: {
    group_by?: 'day' | 'hour'
    start?: string
    end?: string
    provider?: string
    model?: string
  }): Promise<UsageStatsResponse> {
    return request<UsageStatsResponse>(`/api/v1/stats/usage?${toQuery(params).toString()}`)
  },

  async health(windowHours = 1): Promise<HealthStatsResponse> {
    return request<HealthStatsResponse>(`/api/v1/stats/health?window_hours=${windowHours}`)
  },

  async search(params: {
    q: string
    field?: 'request' | 'response'
    provider?: string
    client_type?: string
    user_id?: string
    limit?: number
  }): Promise<{ items: SearchHit[]; total: number; query: string; field: string }> {
    return request(`/api/v1/search?${toQuery(params).toString()}`)
  },

  async errors(params: {
    provider?: string
    client_type?: string
    upstream_status?: number
    start?: string
    end?: string
    limit?: number
    offset?: number
  } = {}): Promise<ErrorListResponse> {
    return request<ErrorListResponse>(`/api/v1/errors?${toQuery(params).toString()}`)
  },

  async users(params: {
    client_type?: string
    min_requests?: number
    days?: number
    order_by?: 'total_tokens' | 'total_requests' | 'last_seen'
    limit?: number
    offset?: number
  } = {}): Promise<UserListResponse> {
    return request<UserListResponse>(`/api/v1/users?${toQuery(params).toString()}`)
  },

  async userProfile(userId: string, days = 30): Promise<UserProfile> {
    return request<UserProfile>(`/api/v1/users/${encodeURIComponent(userId)}?days=${days}`)
  },

  async userTraces(
    userId: string,
    params: { limit?: number; offset?: number; days?: number } = {},
  ): Promise<TraceListResponse> {
    return request<TraceListResponse>(
      `/api/v1/users/${encodeURIComponent(userId)}/traces?${toQuery(params).toString()}`,
    )
  },

  /** 返回导出 URL (仅供展示/copy, 不包含 token; 实际下载用 downloadExport) */
  exportTracesUrl(params: ExportTracesParams = {}): string {
    return `${API_BASE}/api/v1/export/traces?${toQuery(params).toString()}`
  },

  /** 通过 fetch + blob 触发下载 (带 Authorization header, 不依赖 URL 里的 token) */
  async downloadExport(params: ExportTracesParams = {}, filename?: string): Promise<void> {
    const token = getToken()
    const resp = await fetch(`${API_BASE}/api/v1/export/traces?${toQuery(params).toString()}`, {
      headers: token ? { Authorization: `Bearer ${token}` } : {},
    })
    if (!resp.ok) throw new HTTPError(resp.status, `导出失败 HTTP ${resp.status}`)
    const blob = await resp.blob()
    const url = URL.createObjectURL(blob)
    const a = document.createElement('a')
    a.href = url
    a.download = filename || `export-${Date.now()}.${params.format ?? 'jsonl'}`
    document.body.appendChild(a)
    a.click()
    a.remove()
    setTimeout(() => URL.revokeObjectURL(url), 0)
  },

  async ping(): Promise<{ status: string; clickhouse?: boolean }> {
    return request('/readyz')
  },

  /** TokenGate 专用: 验证 token 是否有效, 401 不触发 reload */
  async verifyToken(): Promise<{ status: string }> {
    return request('/api/v1/auth/verify', { _noReloadOn401: true })
  },
}

export { HTTPError }
