import { useEffect, useMemo, useState } from 'react'
import { Link, useParams } from 'react-router-dom'
import {
  ArrowLeft,
  Bot,
  ChevronDown,
  ChevronRight,
  Copy,
  Eye,
  EyeOff,
  MessageSquare,
  Wrench,
  User,
  AlertTriangle,
  Image,
  Mic,
  Video,
  Check,
  Brain,
} from 'lucide-react'
import { api, type TraceDetail } from '@/api/client'
import Badge, { ProviderBadge, StatusBadge } from '@/components/Badge'
import { cn } from '@/lib/utils'
import { formatBytes, formatDuration, formatNumber, formatTime } from '@/lib/utils'

// ============ 类型 ============

interface ChatMessage {
  role: string
  content: string | ContentPart[] | null
  name?: string
  tool_calls?: ToolCallObj[]
  tool_call_id?: string
}

interface ContentPart {
  type: string
  text?: string
  image_url?: { url: string }
  [key: string]: unknown
}

interface ToolCallObj {
  id?: string
  type?: string
  function?: { name: string; arguments: string }
}

interface ToolDef {
  type?: string
  function?: { name: string; description?: string; parameters?: Record<string, unknown> }
}

// ============ JSON helpers ============

function safeParse<T = unknown>(s: string | null | undefined): T | null {
  if (!s) return null
  try {
    return JSON.parse(s) as T
  } catch {
    return null
  }
}

function formatJson(s: string | null | undefined): string {
  if (!s) return ''
  try {
    return JSON.stringify(JSON.parse(s), null, 2)
  } catch {
    return s
  }
}

function copyText(text: string) {
  void navigator.clipboard.writeText(text)
}

// ============ Main component ============

export default function TraceDetailPage() {
  const { requestId = '' } = useParams<{ requestId: string }>()
  const [trace, setTrace] = useState<TraceDetail | null>(null)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState('')
  const [showRaw, setShowRaw] = useState(false)
  const [rawLoading, setRawLoading] = useState(false)
  const [copiedId, setCopiedId] = useState(false)

  async function load(includeRaw = false) {
    if (includeRaw) setRawLoading(true)
    else setLoading(true)
    setError('')
    try {
      const r = await api.getTrace(requestId, includeRaw)
      setTrace(r)
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e))
    } finally {
      setLoading(false)
      setRawLoading(false)
    }
  }

  useEffect(() => {
    void load(false)
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [requestId])

  async function toggleRaw() {
    if (!showRaw && (!trace?.request_json || !trace?.response_json)) {
      await load(true)
    }
    setShowRaw(!showRaw)
  }

  function handleCopyId() {
    copyText(trace?.request_id ?? '')
    setCopiedId(true)
    setTimeout(() => setCopiedId(false), 1500)
  }

  // 从 request_json 解析 messages
  const messages = useMemo<ChatMessage[]>(() => {
    if (!trace?.request_json) return []
    const parsed = safeParse<{ messages?: ChatMessage[] }>(trace.request_json)
    return parsed?.messages ?? []
  }, [trace?.request_json])

  // 从 request_json 解析 tools schema
  const toolDefs = useMemo<ToolDef[]>(() => {
    if (!trace?.request_json) return []
    const parsed = safeParse<{ tools?: ToolDef[] }>(trace.request_json)
    return parsed?.tools ?? []
  }, [trace?.request_json])

  // 从 tool_calls_json 解析 tool calls
  const toolCalls = useMemo<ToolCallObj[]>(() => {
    if (!trace?.tool_calls_json) return []
    const parsed = safeParse<ToolCallObj[]>(trace.tool_calls_json)
    return Array.isArray(parsed) ? parsed : []
  }, [trace?.tool_calls_json])

  // 从 response_json 解析 assistant 响应
  const responseContent = useMemo<string>(() => {
    if (!trace?.response_json) return ''
    const parsed = safeParse<{
      choices?: { message?: { content?: string }; delta?: { content?: string } }[]
      content?: { type?: string; text?: string }[]
    }>(trace.response_json)
    if (parsed?.choices?.[0]?.message?.content) return parsed.choices[0].message.content
    if (parsed?.content) {
      // Anthropic: 只取 text 块, 排除 thinking 块
      return parsed.content
        .filter((c) => c.type !== 'thinking')
        .map((c) => c.text ?? '')
        .filter(Boolean)
        .join('')
    }
    return ''
  }, [trace?.response_json])

  // 从 response_json 解析 thinking / reasoning 内容
  const thinkingContent = useMemo<string>(() => {
    if (!trace?.response_json) return ''
    const parsed = safeParse<{
      choices?: { message?: { reasoning_content?: string } }[]
      content?: { type?: string; thinking?: string }[]
    }>(trace.response_json)
    // OpenAI format — DeepSeek / o1 reasoning_content
    if (parsed?.choices?.[0]?.message?.reasoning_content) {
      return parsed.choices[0].message.reasoning_content
    }
    // Anthropic format — thinking blocks
    if (parsed?.content) {
      const blocks = parsed.content
        .filter((c) => c.type === 'thinking' && c.thinking)
        .map((c) => c.thinking!)
      if (blocks.length > 0) return blocks.join('\n\n---\n\n')
    }
    return ''
  }, [trace?.response_json])

  if (loading) {
    return (
      <div className="p-6 flex items-center gap-2 text-slate-500">
        <div className="w-4 h-4 border-2 border-slate-300 border-t-brand-500 rounded-full animate-spin" />
        加载中...
      </div>
    )
  }
  if (error) {
    return (
      <div className="p-6">
        <div className="bg-red-50 border border-red-200 rounded-lg p-4 text-red-700 text-sm">{error}</div>
      </div>
    )
  }
  if (!trace) return <div className="p-6 text-slate-500">未找到</div>

  const hasConversation = messages.length > 0 || responseContent
  const hasTools = toolDefs.length > 0 || toolCalls.length > 0

  return (
    <div className="p-6 max-w-6xl">
      <Link
        to="/traces"
        className="inline-flex items-center gap-1 text-sm text-slate-600 hover:text-slate-900 mb-4"
      >
        <ArrowLeft className="w-4 h-4" />
        返回列表
      </Link>

      {/* ======== Header card ======== */}
      <div className="bg-white rounded-lg border border-slate-200 p-5 mb-4">
        <div className="flex items-start justify-between flex-wrap gap-3">
          <div>
            <div className="flex items-center gap-2 mb-2">
              <ProviderBadge provider={trace.provider} />
              {trace.is_stream && <Badge variant="gray">SSE</Badge>}
              <StatusBadge status={trace.upstream_status} />
              {trace.is_error && <Badge variant="error">{trace.error_type || 'error'}</Badge>}
            </div>
            <div className="font-mono text-sm text-slate-700">{trace.model}</div>
            <div className="font-mono text-xs text-slate-500 mt-1 flex items-center gap-1">
              {trace.request_id}
              <button
                onClick={handleCopyId}
                className="ml-1 p-0.5 text-slate-400 hover:text-slate-600"
                title="复制 ID"
              >
                {copiedId ? <Check className="w-3 h-3 text-emerald-500" /> : <Copy className="w-3 h-3" />}
              </button>
            </div>
          </div>
          <button
            onClick={() => void toggleRaw()}
            disabled={rawLoading}
            className="flex items-center gap-1 px-3 py-1.5 text-sm bg-brand-500 text-white rounded hover:bg-brand-600 disabled:opacity-50"
          >
            {showRaw ? <EyeOff className="w-4 h-4" /> : <Eye className="w-4 h-4" />}
            {rawLoading ? '加载中...' : showRaw ? '隐藏原文' : '查看原文'}
          </button>
        </div>

        {/* Stats grid */}
        <div className="grid grid-cols-2 md:grid-cols-4 gap-4 mt-5 pt-5 border-t border-slate-200">
          <Stat label="提交时间" value={formatTime(trace.created_at)} />
          <Stat label="完成时间" value={formatTime(trace.completed_at)} />
          <Stat label="总耗时" value={formatDuration(trace.duration_ms)} />
          {trace.is_stream && trace.ttft_ms > 0 && (
            <Stat label="首 token (TTFT)" value={`${trace.ttft_ms}ms`} />
          )}
          <Stat label="Prompt tokens" value={formatNumber(trace.prompt_tokens)} />
          <Stat label="Completion tokens" value={formatNumber(trace.completion_tokens)} />
          {(trace.cached_tokens ?? 0) > 0 && (
            <Stat label="Cached tokens" value={formatNumber(trace.cached_tokens)} />
          )}
          {(trace.reasoning_tokens ?? 0) > 0 && (
            <Stat label="Reasoning tokens" value={formatNumber(trace.reasoning_tokens)} />
          )}
          <Stat label="Request size" value={formatBytes(trace.request_size_bytes || 0)} />
          <Stat label="Response size" value={formatBytes(trace.response_size_bytes || 0)} />
          {(trace.response_chunk_count ?? 0) > 0 && (
            <Stat label="Stream chunks" value={formatNumber(trace.response_chunk_count)} />
          )}
          <Stat label="Finish reason" value={trace.response_finish_reason || '-'} />
        </div>
      </div>

      {/* ======== Error section ======== */}
      {trace.is_error && (
        <div className="bg-red-50 border border-red-200 rounded-lg p-5 mb-4">
          <div className="flex items-center gap-2 mb-2">
            <AlertTriangle className="w-4 h-4 text-red-600" />
            <h3 className="text-sm font-semibold text-red-800">错误信息</h3>
            <Badge variant="error">{trace.error_type || 'unknown'}</Badge>
          </div>
          <pre className="text-xs bg-red-100 rounded p-3 overflow-x-auto whitespace-pre-wrap text-red-800 font-mono">
            {trace.error_message || '(无详情)'}
          </pre>
        </div>
      )}

      {/* ======== Multi-turn Conversation ======== */}
      {hasConversation && (
        <CollapsibleSection
          title="对话内容"
          icon={<MessageSquare className="w-4 h-4" />}
          defaultOpen
          badge={`${messages.length} messages`}
        >
          <div className="space-y-3">
            {messages.map((msg, i) => (
              <MessageBubble key={i} msg={msg} />
            ))}
            {responseContent && !messages.some((m) => m.role === 'assistant' && m.content) && (
              <MessageBubble
                msg={{ role: 'assistant', content: responseContent }}
              />
            )}
          </div>
        </CollapsibleSection>
      )}

      {/* ======== Thinking / Reasoning ======== */}
      {thinkingContent && (
        <CollapsibleSection
          title="思考过程 (Thinking)"
          icon={<Brain className="w-4 h-4" />}
          badge={`${thinkingContent.length} chars`}
        >
          <pre className="text-xs whitespace-pre-wrap font-mono text-amber-900 bg-amber-50 border border-amber-200 rounded-lg p-4 max-h-[32rem] overflow-y-auto">
            {thinkingContent}
          </pre>
        </CollapsibleSection>
      )}

      {/* ======== Tool Definitions ======== */}
      {hasTools && (
        <CollapsibleSection
          title="Tool 调用"
          icon={<Wrench className="w-4 h-4" />}
          badge={toolCalls.length > 0 ? `${toolCalls.length} calls` : undefined}
        >
          {toolDefs.length > 0 && (
            <div className="mb-4">
              <div className="text-xs font-medium text-slate-500 mb-2 uppercase tracking-wider">
                可用 Tools ({toolDefs.length})
              </div>
              <div className="grid gap-2">
                {toolDefs.map((t, i) => (
                  <ToolDefCard key={i} tool={t} />
                ))}
              </div>
            </div>
          )}
          {toolCalls.length > 0 && (
            <div>
              <div className="text-xs font-medium text-slate-500 mb-2 uppercase tracking-wider">
                实际调用 ({toolCalls.length})
              </div>
              <div className="grid gap-2">
                {toolCalls.map((tc, i) => (
                  <ToolCallCard key={i} call={tc} />
                ))}
              </div>
            </div>
          )}
        </CollapsibleSection>
      )}

      {/* ======== Preview (always visible) ======== */}
      {!hasConversation && (
        <CollapsibleSection title="Preview (截断)" defaultOpen>
          <div className="space-y-3">
            <div>
              <div className="text-xs text-slate-500 mb-1">Request preview</div>
              <pre className="text-xs bg-slate-50 border border-slate-200 rounded p-3 overflow-x-auto whitespace-pre-wrap font-mono max-h-60">
                {trace.request_text_preview || '(空)'}
              </pre>
            </div>
            <div>
              <div className="text-xs text-slate-500 mb-1">Response preview</div>
              <pre className="text-xs bg-slate-50 border border-slate-200 rounded p-3 overflow-x-auto whitespace-pre-wrap font-mono max-h-60">
                {trace.response_text_preview || '(空)'}
              </pre>
            </div>
          </div>
        </CollapsibleSection>
      )}

      {/* ======== Client info ======== */}
      <CollapsibleSection title="客户端信息">
        <KV k="user_id" v={trace.user_id || '-'} />
        <KV k="api_key_prefix" v={trace.api_key_prefix || '-'} />
        <KV k="api_key_hash" v={<code className="text-xs">{trace.api_key_hash || '-'}</code>} />
        <KV k="organization_id" v={trace.organization_id || '-'} />
        <KV k="client_ip" v={trace.client_ip || '-'} />
        <KV k="user_agent" v={<span className="text-xs">{trace.client_user_agent || '-'}</span>} />
      </CollapsibleSection>

      {/* ======== Request structure ======== */}
      <CollapsibleSection title="请求结构">
        <KV k="endpoint" v={`${trace.http_method} ${trace.endpoint}`} />
        <KV k="message_count" v={trace.request_message_count ?? '-'} />
        <KV k="temperature" v={trace.request_temperature?.toString() ?? '-'} />
        <KV k="top_p" v={trace.request_top_p?.toString() ?? '-'} />
        <KV k="max_tokens" v={trace.request_max_tokens?.toString() ?? '-'} />
        <KV
          k="多模态"
          v={
            <div className="flex gap-1">
              {trace.request_has_image && (
                <Badge variant="info"><Image className="w-3 h-3 mr-0.5 inline" />image</Badge>
              )}
              {trace.request_has_audio && (
                <Badge variant="info"><Mic className="w-3 h-3 mr-0.5 inline" />audio</Badge>
              )}
              {trace.request_has_video && (
                <Badge variant="info"><Video className="w-3 h-3 mr-0.5 inline" />video</Badge>
              )}
              {trace.request_has_tool && (
                <Badge variant="warning"><Wrench className="w-3 h-3 mr-0.5 inline" />tool</Badge>
              )}
              {!trace.request_has_image &&
                !trace.request_has_audio &&
                !trace.request_has_video &&
                !trace.request_has_tool &&
                '-'}
            </div>
          }
        />
      </CollapsibleSection>

      {/* ======== Raw JSON ======== */}
      {showRaw && (
        <CollapsibleSection title="原始 JSON" defaultOpen>
          <RawBlock label="request_json" content={trace.request_json} cosKey={trace.request_cos_key} />
          <div className="mt-3">
            <RawBlock
              label="response_json"
              content={trace.response_json}
              cosKey={trace.response_cos_key}
            />
          </div>
          {trace.tool_calls_json && (
            <div className="mt-3">
              <RawBlock label="tool_calls_json" content={trace.tool_calls_json} />
            </div>
          )}
        </CollapsibleSection>
      )}
    </div>
  )
}

// ============ Sub-components ============

function Stat({ label, value }: { label: string; value: React.ReactNode }) {
  return (
    <div>
      <div className="text-xs text-slate-500">{label}</div>
      <div className="text-sm font-medium text-slate-900 mt-0.5">{value}</div>
    </div>
  )
}

function CollapsibleSection({
  title,
  icon,
  badge,
  defaultOpen = false,
  children,
}: {
  title: string
  icon?: React.ReactNode
  badge?: string
  defaultOpen?: boolean
  children: React.ReactNode
}) {
  const [open, setOpen] = useState(defaultOpen)
  return (
    <div className="bg-white rounded-lg border border-slate-200 mb-4 overflow-hidden">
      <button
        onClick={() => setOpen(!open)}
        className="w-full flex items-center gap-2 p-4 text-left hover:bg-slate-50 transition-colors"
      >
        {open ? (
          <ChevronDown className="w-4 h-4 text-slate-400" />
        ) : (
          <ChevronRight className="w-4 h-4 text-slate-400" />
        )}
        {icon}
        <h3 className="text-sm font-semibold text-slate-900 flex-1">{title}</h3>
        {badge && <Badge variant="gray">{badge}</Badge>}
      </button>
      {open && <div className="px-5 pb-5 border-t border-slate-100">{children}</div>}
    </div>
  )
}

function KV({ k, v }: { k: string; v: React.ReactNode }) {
  return (
    <div className="grid grid-cols-3 gap-2 py-1.5 text-sm border-b border-slate-100 last:border-0">
      <div className="text-slate-500">{k}</div>
      <div className="col-span-2 text-slate-800 break-all">{v}</div>
    </div>
  )
}

/** 聊天消息气泡 */
function MessageBubble({ msg }: { msg: ChatMessage }) {
  const isUser = msg.role === 'user'
  const isSystem = msg.role === 'system'
  const isAssistant = msg.role === 'assistant'
  const isTool = msg.role === 'tool'

  const roleIcon = isUser ? (
    <User className="w-4 h-4" />
  ) : isAssistant ? (
    <Bot className="w-4 h-4" />
  ) : isTool ? (
    <Wrench className="w-4 h-4" />
  ) : (
    <MessageSquare className="w-4 h-4" />
  )

  const roleBg = isUser
    ? 'bg-blue-50 border-blue-200'
    : isAssistant
      ? 'bg-emerald-50 border-emerald-200'
      : isSystem
        ? 'bg-amber-50 border-amber-200'
        : 'bg-slate-50 border-slate-200'

  const roleLabel = isUser
    ? 'text-blue-700'
    : isAssistant
      ? 'text-emerald-700'
      : isSystem
        ? 'text-amber-700'
        : 'text-slate-600'

  // 提取文本内容
  let textContent = ''
  if (typeof msg.content === 'string') {
    textContent = msg.content
  } else if (Array.isArray(msg.content)) {
    textContent = msg.content.map((p) => p.text ?? `[${p.type}]`).join('\n')
  }

  return (
    <div className={cn('rounded-lg border p-3', roleBg)}>
      <div className={cn('flex items-center gap-1.5 text-xs font-medium mb-1.5', roleLabel)}>
        {roleIcon}
        <span className="uppercase">{msg.role}</span>
        {msg.name && <Badge variant="gray">{msg.name}</Badge>}
        {msg.tool_call_id && <Badge variant="gray">call: {msg.tool_call_id}</Badge>}
      </div>
      {textContent && (
        <pre className="text-xs whitespace-pre-wrap font-mono text-slate-800 max-h-96 overflow-y-auto">
          {textContent}
        </pre>
      )}
      {/* tool_calls 内联在 assistant 消息中 */}
      {msg.tool_calls && msg.tool_calls.length > 0 && (
        <div className="mt-2 space-y-1">
          {msg.tool_calls.map((tc, i) => (
            <div key={i} className="text-xs bg-white/70 rounded p-2 border border-slate-200">
              <div className="flex items-center gap-1 font-medium text-slate-700">
                <Wrench className="w-3 h-3" />
                {tc.function?.name ?? 'unknown'}
              </div>
              {tc.function?.arguments && (
                <pre className="mt-1 text-slate-600 font-mono whitespace-pre-wrap max-h-40 overflow-y-auto">
                  {formatJson(tc.function.arguments)}
                </pre>
              )}
            </div>
          ))}
        </div>
      )}
      {/* 多模态内容部分 */}
      {Array.isArray(msg.content) &&
        msg.content.some((p) => p.type === 'image_url') && (
          <div className="mt-2 flex gap-2 flex-wrap">
            {msg.content
              .filter((p) => p.type === 'image_url')
              .map((p, i) => (
                <div
                  key={i}
                  className="w-20 h-20 bg-slate-200 rounded flex items-center justify-center text-slate-400"
                  title={p.image_url?.url ?? ''}
                >
                  <Image className="w-6 h-6" />
                </div>
              ))}
          </div>
        )}
    </div>
  )
}

/** Tool definition 卡片 */
function ToolDefCard({ tool }: { tool: ToolDef }) {
  const [expanded, setExpanded] = useState(false)
  const fn = tool.function
  if (!fn) return null

  return (
    <div className="border border-slate-200 rounded-lg overflow-hidden">
      <button
        onClick={() => setExpanded(!expanded)}
        className="w-full flex items-center gap-2 px-3 py-2 text-left hover:bg-slate-50 text-sm"
      >
        {expanded ? (
          <ChevronDown className="w-3 h-3 text-slate-400" />
        ) : (
          <ChevronRight className="w-3 h-3 text-slate-400" />
        )}
        <Wrench className="w-3.5 h-3.5 text-amber-600" />
        <span className="font-mono font-medium text-slate-800">{fn.name}</span>
        {fn.description && (
          <span className="text-xs text-slate-500 truncate flex-1 ml-2">
            {fn.description}
          </span>
        )}
      </button>
      {expanded && fn.parameters && (
        <div className="px-3 pb-3 border-t border-slate-100">
          <pre className="text-xs bg-slate-50 rounded p-2 mt-2 overflow-x-auto whitespace-pre-wrap font-mono text-slate-700 max-h-60">
            {JSON.stringify(fn.parameters, null, 2)}
          </pre>
        </div>
      )}
    </div>
  )
}

/** Tool call 结果卡片 */
function ToolCallCard({ call }: { call: ToolCallObj }) {
  const [expanded, setExpanded] = useState(true)
  return (
    <div className="border border-emerald-200 bg-emerald-50/50 rounded-lg overflow-hidden">
      <button
        onClick={() => setExpanded(!expanded)}
        className="w-full flex items-center gap-2 px-3 py-2 text-left hover:bg-emerald-50 text-sm"
      >
        {expanded ? (
          <ChevronDown className="w-3 h-3 text-emerald-500" />
        ) : (
          <ChevronRight className="w-3 h-3 text-emerald-500" />
        )}
        <Wrench className="w-3.5 h-3.5 text-emerald-600" />
        <span className="font-mono font-medium text-slate-800">
          {call.function?.name ?? 'unknown'}
        </span>
        {call.id && <Badge variant="gray">{call.id}</Badge>}
      </button>
      {expanded && call.function?.arguments && (
        <div className="px-3 pb-3 border-t border-emerald-100">
          <pre className="text-xs bg-white rounded p-2 mt-2 overflow-x-auto whitespace-pre-wrap font-mono text-slate-700 max-h-60">
            {formatJson(call.function.arguments)}
          </pre>
        </div>
      )}
    </div>
  )
}

function RawBlock({
  label,
  content,
  cosKey,
}: {
  label: string
  content: string | null | undefined
  cosKey?: string
}) {
  return (
    <div>
      <div className="flex items-center justify-between mb-1">
        <div className="text-xs text-slate-500">{label}</div>
        <div className="flex items-center gap-2">
          {cosKey && <Badge variant="gray">COS: {cosKey}</Badge>}
          {content && (
            <button
              onClick={() => copyText(formatJson(content))}
              className="text-xs text-slate-500 hover:text-slate-700 flex items-center gap-0.5"
            >
              <Copy className="w-3 h-3" /> 复制
            </button>
          )}
        </div>
      </div>
      <pre className="text-xs bg-slate-900 text-slate-100 rounded p-3 overflow-x-auto whitespace-pre-wrap font-mono max-h-[32rem]">
        {formatJson(content) || '(空)'}
      </pre>
    </div>
  )
}
