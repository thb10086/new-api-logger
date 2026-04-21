import { useEffect, useState } from 'react'
import { Link } from 'react-router-dom'
import { AlertTriangle, ExternalLink, RefreshCw } from 'lucide-react'
import { api, type ErrorRow } from '@/api/client'
import Badge, { ProviderBadge, StatusBadge } from '@/components/Badge'
import ClientTypeFilter, { CLIENT_TYPE_LABEL } from '@/components/ClientTypeFilter'
import { formatDuration, formatTime, truncate } from '@/lib/utils'

export default function ErrorsPage() {
  const [rows, setRows] = useState<ErrorRow[]>([])
  const [total, setTotal] = useState(0)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState('')
  const [provider, setProvider] = useState('')
  const [clientType, setClientType] = useState('')
  const [offset, setOffset] = useState(0)
  const limit = 50

  async function load() {
    setLoading(true)
    setError('')
    try {
      const r = await api.errors({
        provider: provider || undefined,
        client_type: clientType || undefined,
        limit,
        offset,
      })
      setRows(r.items)
      setTotal(r.total)
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e))
    } finally {
      setLoading(false)
    }
  }

  useEffect(() => {
    void load()
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [offset, provider, clientType])

  return (
    <div className="p-6">
      <div className="flex items-center justify-between mb-6">
        <div>
          <h2 className="text-2xl font-semibold text-slate-900">错误诊断</h2>
          <p className="text-sm text-slate-500 mt-1">近期错误请求 (近 30 天)</p>
        </div>
        <div className="flex items-center gap-3">
          <ClientTypeFilter
            value={clientType}
            onChange={(v) => {
              setClientType(v)
              setOffset(0)
            }}
            className="px-3 py-1.5 text-sm border border-slate-300 rounded-md w-40 bg-white"
          />
          <input
            type="text"
            value={provider}
            onChange={(e) => {
              setProvider(e.target.value)
              setOffset(0)
            }}
            placeholder="Provider 过滤"
            className="px-3 py-1.5 text-sm border border-slate-300 rounded-md w-36"
          />
          <button
            onClick={() => void load()}
            disabled={loading}
            className="flex items-center gap-1 px-3 py-1.5 text-sm border border-slate-300 rounded-md hover:bg-slate-50"
          >
            <RefreshCw className={`w-4 h-4 ${loading ? 'animate-spin' : ''}`} />
            刷新
          </button>
        </div>
      </div>

      {error && (
        <div className="mb-4 px-4 py-3 bg-red-50 border border-red-200 text-red-700 text-sm rounded-lg">
          {error}
        </div>
      )}

      <div className="bg-white rounded-lg border border-slate-200 overflow-hidden">
        <table className="w-full text-sm">
          <thead className="bg-slate-50 border-b border-slate-200">
            <tr>
              <th className="text-left px-4 py-3 font-medium text-slate-700">时间</th>
              <th className="text-left px-4 py-3 font-medium text-slate-700">Provider</th>
              <th className="text-left px-4 py-3 font-medium text-slate-700">客户端</th>
              <th className="text-left px-4 py-3 font-medium text-slate-700">Model</th>
              <th className="text-left px-4 py-3 font-medium text-slate-700">Status</th>
              <th className="text-left px-4 py-3 font-medium text-slate-700">Error Type</th>
              <th className="text-left px-4 py-3 font-medium text-slate-700">错误信息</th>
              <th className="text-left px-4 py-3 font-medium text-slate-700">耗时</th>
              <th className="text-left px-4 py-3 font-medium text-slate-700" />
            </tr>
          </thead>
          <tbody className="divide-y divide-slate-100">
            {loading && rows.length === 0 ? (
              <tr>
                <td colSpan={9} className="px-4 py-12 text-center text-slate-400">
                  加载中...
                </td>
              </tr>
            ) : rows.length === 0 ? (
              <tr>
                <td colSpan={9} className="px-4 py-12 text-center text-slate-400">
                  <AlertTriangle className="w-6 h-6 mx-auto mb-2 opacity-50" />
                  没有错误记录
                </td>
              </tr>
            ) : (
              rows.map((row) => (
                <tr key={row.request_id} className="hover:bg-slate-50">
                  <td className="px-4 py-3 text-xs text-slate-600 whitespace-nowrap">
                    {formatTime(row.created_at)}
                  </td>
                  <td className="px-4 py-3">
                    <ProviderBadge provider={row.provider} />
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
                  <td className="px-4 py-3 font-mono text-xs text-slate-700">
                    {row.model}
                  </td>
                  <td className="px-4 py-3">
                    <StatusBadge status={row.upstream_status} />
                  </td>
                  <td className="px-4 py-3">
                    <Badge variant="error">{row.error_type || '-'}</Badge>
                  </td>
                  <td className="px-4 py-3 text-xs text-slate-600 max-w-xs">
                    {truncate(row.error_message || '-', 80)}
                  </td>
                  <td className="px-4 py-3 text-xs text-slate-600 whitespace-nowrap">
                    {formatDuration(row.duration_ms)}
                  </td>
                  <td className="px-4 py-3">
                    <Link
                      to={`/traces/${row.request_id}`}
                      className="text-brand-500 hover:text-brand-700"
                    >
                      <ExternalLink className="w-4 h-4" />
                    </Link>
                  </td>
                </tr>
              ))
            )}
          </tbody>
        </table>

        {/* Pagination */}
        {total > limit && (
          <div className="flex items-center justify-between px-4 py-3 border-t border-slate-200 bg-slate-50">
            <div className="text-xs text-slate-500">
              显示 {offset + 1}-{Math.min(offset + limit, total)} / {total}
            </div>
            <div className="flex gap-2">
              <button
                onClick={() => setOffset(Math.max(0, offset - limit))}
                disabled={offset === 0}
                className="px-3 py-1 text-xs border border-slate-300 rounded hover:bg-white disabled:opacity-50"
              >
                上一页
              </button>
              <button
                onClick={() => setOffset(offset + limit)}
                disabled={offset + limit >= total}
                className="px-3 py-1 text-xs border border-slate-300 rounded hover:bg-white disabled:opacity-50"
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
