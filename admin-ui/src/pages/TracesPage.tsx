import { useEffect, useState } from 'react'
import { Link } from 'react-router-dom'
import { Download, Filter, RefreshCw, Search } from 'lucide-react'
import { api, type TraceListResponse } from '@/api/client'
import Badge, { ProviderBadge, StatusBadge } from '@/components/Badge'
import ClientTypeFilter, { CLIENT_TYPE_LABEL } from '@/components/ClientTypeFilter'
import { formatDuration, formatNumber, formatTime, truncate } from '@/lib/utils'

const PAGE_SIZE = 50

const PROVIDERS = [
  { value: '', label: '全部 provider' },
  { value: 'openai_chat', label: 'OpenAI Chat' },
  { value: 'openai_responses', label: 'OpenAI Responses' },
  { value: 'anthropic', label: 'Anthropic' },
  { value: 'gemini', label: 'Gemini' },
]

export default function TracesPage() {
  const [data, setData] = useState<TraceListResponse | null>(null)
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState<string>('')

  // 过滤
  const [provider, setProvider] = useState('')
  const [model, setModel] = useState('')
  const [userId, setUserId] = useState('')
  const [clientType, setClientType] = useState('')
  const [isError, setIsError] = useState<'all' | 'true' | 'false'>('all')
  const [offset, setOffset] = useState(0)
  const [exporting, setExporting] = useState(false)

  async function load() {
    setLoading(true)
    setError('')
    try {
      const r = await api.listTraces({
        provider: provider || undefined,
        model: model || undefined,
        user_id: userId || undefined,
        client_type: clientType || undefined,
        is_error: isError === 'all' ? undefined : isError === 'true',
        limit: PAGE_SIZE,
        offset,
      })
      setData(r)
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e))
    } finally {
      setLoading(false)
    }
  }

  async function handleExport(format: 'jsonl' | 'csv') {
    setExporting(true)
    setError('')
    try {
      await api.downloadExport(
        {
          format,
          provider: provider || undefined,
          client_type: clientType || undefined,
          user_id: userId || undefined,
          is_error: isError === 'all' ? undefined : isError === 'true',
          max_rows: 100_000,
        },
        `traces-${new Date().toISOString().slice(0, 10)}.${format}`,
      )
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e))
    } finally {
      setExporting(false)
    }
  }

  useEffect(() => {
    void load()
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [offset])

  function handleSearch() {
    setOffset(0)
    void load()
  }

  return (
    <div className="p-6">
      {/* Header */}
      <div className="flex items-center justify-between mb-6">
        <div>
          <h2 className="text-2xl font-semibold text-slate-900">请求 Traces</h2>
          <p className="text-sm text-slate-500 mt-1">浏览所有 LLM 调用记录</p>
        </div>
        <div className="flex items-center gap-2">
          <button
            onClick={() => void handleExport('jsonl')}
            disabled={exporting}
            className="flex items-center gap-2 px-3 py-1.5 text-sm bg-white border border-slate-300 rounded-lg hover:bg-slate-50 disabled:opacity-50"
            title="按当前过滤条件导出 JSONL (最多 10 万条)"
          >
            <Download className="w-4 h-4" />
            导出 JSONL
          </button>
          <button
            onClick={() => void handleExport('csv')}
            disabled={exporting}
            className="flex items-center gap-2 px-3 py-1.5 text-sm bg-white border border-slate-300 rounded-lg hover:bg-slate-50 disabled:opacity-50"
          >
            <Download className="w-4 h-4" />
            导出 CSV
          </button>
          <button
            onClick={() => void load()}
            className="flex items-center gap-2 px-3 py-1.5 text-sm bg-white border border-slate-300 rounded-lg hover:bg-slate-50"
          >
            <RefreshCw className={`w-4 h-4 ${loading ? 'animate-spin' : ''}`} />
            刷新
          </button>
        </div>
      </div>

      {/* Filter bar */}
      <div className="bg-white rounded-lg border border-slate-200 p-4 mb-4">
        <div className="grid grid-cols-1 md:grid-cols-6 gap-3">
          <select
            value={provider}
            onChange={(e) => setProvider(e.target.value)}
            className="px-3 py-2 border border-slate-300 rounded-md text-sm"
          >
            {PROVIDERS.map((p) => (
              <option key={p.value} value={p.value}>
                {p.label}
              </option>
            ))}
          </select>
          <ClientTypeFilter
            value={clientType}
            onChange={setClientType}
            className="px-3 py-2 border border-slate-300 rounded-md text-sm bg-white"
          />
          <input
            type="text"
            placeholder="model 精确匹配"
            value={model}
            onChange={(e) => setModel(e.target.value)}
            className="px-3 py-2 border border-slate-300 rounded-md text-sm"
          />
          <input
            type="text"
            placeholder="user_id"
            value={userId}
            onChange={(e) => setUserId(e.target.value)}
            className="px-3 py-2 border border-slate-300 rounded-md text-sm"
          />
          <select
            value={isError}
            onChange={(e) => setIsError(e.target.value as typeof isError)}
            className="px-3 py-2 border border-slate-300 rounded-md text-sm"
          >
            <option value="all">所有结果</option>
            <option value="false">仅成功</option>
            <option value="true">仅错误</option>
          </select>
          <button
            onClick={handleSearch}
            className="flex items-center justify-center gap-2 px-4 py-2 bg-brand-500 text-white text-sm rounded-md hover:bg-brand-600"
          >
            <Search className="w-4 h-4" />
            查询
          </button>
        </div>
      </div>

      {/* Error */}
      {error && (
        <div className="mb-4 px-4 py-3 bg-red-50 border border-red-200 text-red-700 text-sm rounded-lg">
          {error}
        </div>
      )}

      {/* Table */}
      <div className="bg-white rounded-lg border border-slate-200 overflow-hidden">
        <div className="overflow-x-auto">
          <table className="w-full text-sm">
            <thead className="bg-slate-50 border-b border-slate-200">
              <tr className="text-left text-xs uppercase text-slate-500">
                <th className="px-4 py-3">时间</th>
                <th className="px-4 py-3">Provider</th>
                <th className="px-4 py-3">客户端</th>
                <th className="px-4 py-3">Model</th>
                <th className="px-4 py-3">User</th>
                <th className="px-4 py-3">Status</th>
                <th className="px-4 py-3 text-right">Tokens</th>
                <th className="px-4 py-3 text-right">耗时</th>
                <th className="px-4 py-3">Preview</th>
              </tr>
            </thead>
            <tbody className="divide-y divide-slate-100">
              {data?.items.map((row) => (
                <tr key={row.request_id} className="hover:bg-slate-50">
                  <td className="px-4 py-3 whitespace-nowrap text-slate-600">
                    <Link
                      to={`/traces/${row.request_id}`}
                      className="text-brand-600 hover:underline font-mono text-xs"
                      title={row.request_id}
                    >
                      {formatTime(row.created_at)}
                    </Link>
                  </td>
                  <td className="px-4 py-3">
                    <ProviderBadge provider={row.provider} />
                    {row.is_stream && (
                      <Badge variant="gray" className="ml-1">
                        SSE
                      </Badge>
                    )}
                  </td>
                  <td className="px-4 py-3">
                    {row.client_type ? (
                      <Badge variant={row.client_type === 'claude_code' ? 'purple' : 'gray'}>
                        {CLIENT_TYPE_LABEL[row.client_type] || row.client_type}
                      </Badge>
                    ) : (
                      <span className="text-xs text-slate-400">-</span>
                    )}
                  </td>
                  <td className="px-4 py-3 font-mono text-xs text-slate-700">{row.model}</td>
                  <td className="px-4 py-3 text-slate-600">{row.user_id || '-'}</td>
                  <td className="px-4 py-3">
                    <StatusBadge status={row.upstream_status} />
                    {row.is_error && (
                      <Badge variant="error" className="ml-1">
                        ERR
                      </Badge>
                    )}
                  </td>
                  <td className="px-4 py-3 text-right text-slate-700">
                    <span className="font-medium">{formatNumber(row.total_tokens)}</span>
                    <div className="text-xs text-slate-400">
                      {formatNumber(row.prompt_tokens)} / {formatNumber(row.completion_tokens)}
                    </div>
                  </td>
                  <td className="px-4 py-3 text-right text-slate-700">
                    {formatDuration(row.duration_ms)}
                    {row.is_stream && row.ttft_ms > 0 && (
                      <div className="text-xs text-slate-400">TTFT {row.ttft_ms}ms</div>
                    )}
                  </td>
                  <td className="px-4 py-3 text-slate-600 max-w-md">
                    <div className="truncate" title={row.request_text_preview || ''}>
                      {truncate(row.request_text_preview, 80) || '-'}
                    </div>
                  </td>
                </tr>
              ))}
              {data?.items.length === 0 && (
                <tr>
                  <td colSpan={9} className="px-4 py-8 text-center text-slate-400">
                    <Filter className="w-6 h-6 mx-auto mb-2 opacity-50" />
                    没有匹配的记录
                  </td>
                </tr>
              )}
            </tbody>
          </table>
        </div>

        {/* Pagination */}
        {data && data.total > PAGE_SIZE && (
          <div className="flex items-center justify-between px-4 py-3 border-t border-slate-200 text-sm">
            <div className="text-slate-500">
              共 {formatNumber(data.total)} 条 · 显示 {offset + 1}-
              {Math.min(offset + PAGE_SIZE, data.total)}
            </div>
            <div className="flex gap-2">
              <button
                onClick={() => setOffset(Math.max(0, offset - PAGE_SIZE))}
                disabled={offset === 0}
                className="px-3 py-1.5 border border-slate-300 rounded-md text-sm disabled:opacity-50"
              >
                上一页
              </button>
              <button
                onClick={() => setOffset(offset + PAGE_SIZE)}
                disabled={offset + PAGE_SIZE >= data.total}
                className="px-3 py-1.5 border border-slate-300 rounded-md text-sm disabled:opacity-50"
              >
                下一页
              </button>
            </div>
          </div>
        )}
      </div>
    </div>
  )
}
