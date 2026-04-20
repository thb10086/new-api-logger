import { useEffect, useState } from 'react'
import { Link, useParams } from 'react-router-dom'
import { ArrowLeft, Copy, Eye, EyeOff } from 'lucide-react'
import { api, type TraceDetail } from '@/api/client'
import Badge, { ProviderBadge, StatusBadge } from '@/components/Badge'
import { formatBytes, formatDuration, formatNumber, formatTime } from '@/lib/utils'

export default function TraceDetailPage() {
  const { requestId = '' } = useParams<{ requestId: string }>()
  const [trace, setTrace] = useState<TraceDetail | null>(null)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState('')
  const [showRaw, setShowRaw] = useState(false)
  const [rawLoading, setRawLoading] = useState(false)

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

  if (loading) return <div className="p-6 text-slate-500">加载中...</div>
  if (error) return <div className="p-6 text-red-600">{error}</div>
  if (!trace) return <div className="p-6 text-slate-500">未找到</div>

  return (
    <div className="p-6 max-w-5xl">
      <Link
        to="/traces"
        className="inline-flex items-center gap-1 text-sm text-slate-600 hover:text-slate-900 mb-4"
      >
        <ArrowLeft className="w-4 h-4" />
        返回列表
      </Link>

      {/* Header */}
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
            <div className="font-mono text-xs text-slate-500 mt-1">{trace.request_id}</div>
          </div>
          <div className="flex items-center gap-2">
            <button
              onClick={() => void navigator.clipboard.writeText(trace.request_id)}
              className="flex items-center gap-1 px-2 py-1 text-xs border border-slate-300 rounded hover:bg-slate-50"
            >
              <Copy className="w-3 h-3" /> ID
            </button>
            <button
              onClick={() => void toggleRaw()}
              disabled={rawLoading}
              className="flex items-center gap-1 px-3 py-1.5 text-sm bg-brand-500 text-white rounded hover:bg-brand-600 disabled:opacity-50"
            >
              {showRaw ? <EyeOff className="w-4 h-4" /> : <Eye className="w-4 h-4" />}
              {rawLoading ? '加载中…' : showRaw ? '隐藏原文' : '查看原文'}
            </button>
          </div>
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
          <Stat label="Cached tokens" value={formatNumber(trace.cached_tokens)} />
          <Stat label="Reasoning tokens" value={formatNumber(trace.reasoning_tokens)} />
          <Stat label="Request size" value={formatBytes(trace.request_size_bytes || 0)} />
          <Stat label="Response size" value={formatBytes(trace.response_size_bytes || 0)} />
          <Stat label="Stream chunks" value={formatNumber(trace.response_chunk_count)} />
          <Stat label="Finish reason" value={trace.response_finish_reason || '-'} />
        </div>
      </div>

      {/* Client info */}
      <Section title="客户端信息">
        <KV k="user_id" v={trace.user_id || '-'} />
        <KV k="api_key_prefix" v={trace.api_key_prefix || '-'} />
        <KV k="api_key_hash" v={<code className="text-xs">{trace.api_key_hash || '-'}</code>} />
        <KV k="organization_id" v={trace.organization_id || '-'} />
        <KV k="client_ip" v={trace.client_ip || '-'} />
        <KV k="user_agent" v={<span className="text-xs">{trace.client_user_agent || '-'}</span>} />
      </Section>

      {/* 请求结构 */}
      <Section title="请求结构">
        <KV k="endpoint" v={`${trace.http_method} ${trace.endpoint}`} />
        <KV k="message_count" v={trace.request_message_count ?? '-'} />
        <KV k="temperature" v={trace.request_temperature?.toString() ?? '-'} />
        <KV k="top_p" v={trace.request_top_p?.toString() ?? '-'} />
        <KV k="max_tokens" v={trace.request_max_tokens?.toString() ?? '-'} />
        <KV
          k="多模态"
          v={
            <div className="flex gap-1">
              {trace.request_has_image && <Badge variant="info">image</Badge>}
              {trace.request_has_audio && <Badge variant="info">audio</Badge>}
              {trace.request_has_video && <Badge variant="info">video</Badge>}
              {trace.request_has_tool && <Badge variant="warning">tool</Badge>}
              {!trace.request_has_image &&
                !trace.request_has_audio &&
                !trace.request_has_video &&
                !trace.request_has_tool &&
                '-'}
            </div>
          }
        />
      </Section>

      {/* Preview */}
      <Section title="Preview (截断)">
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
      </Section>

      {/* Error */}
      {trace.is_error && (
        <Section title="错误信息">
          <KV k="error_type" v={<Badge variant="error">{trace.error_type || '-'}</Badge>} />
          <div className="mt-2">
            <pre className="text-xs bg-red-50 border border-red-200 rounded p-3 overflow-x-auto whitespace-pre-wrap text-red-800">
              {trace.error_message || '(无详情)'}
            </pre>
          </div>
        </Section>
      )}

      {/* Raw JSON */}
      {showRaw && (
        <Section title="原始 JSON">
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
        </Section>
      )}
    </div>
  )
}

function Stat({ label, value }: { label: string; value: React.ReactNode }) {
  return (
    <div>
      <div className="text-xs text-slate-500">{label}</div>
      <div className="text-sm font-medium text-slate-900 mt-0.5">{value}</div>
    </div>
  )
}

function Section({ title, children }: { title: string; children: React.ReactNode }) {
  return (
    <div className="bg-white rounded-lg border border-slate-200 p-5 mb-4">
      <h3 className="text-sm font-semibold text-slate-900 mb-3">{title}</h3>
      {children}
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
        {cosKey && <Badge variant="gray">COS: {cosKey}</Badge>}
      </div>
      <pre className="text-xs bg-slate-900 text-slate-100 rounded p-3 overflow-x-auto whitespace-pre-wrap font-mono max-h-96">
        {formatJson(content) || '(空)'}
      </pre>
    </div>
  )
}

function formatJson(s: string | null | undefined): string {
  if (!s) return ''
  try {
    return JSON.stringify(JSON.parse(s), null, 2)
  } catch {
    return s
  }
}
