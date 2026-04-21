import { useEffect, useState } from 'react'
import { Link } from 'react-router-dom'
import { Download, RefreshCw, Users } from 'lucide-react'
import { api, type UserProfile } from '@/api/client'
import Badge, { ProviderBadge } from '@/components/Badge'
import ClientTypeFilter, { CLIENT_TYPE_LABEL } from '@/components/ClientTypeFilter'
import { formatBytes, formatNumber, formatTime, truncate } from '@/lib/utils'

const PAGE_SIZE = 50

export default function UsersPage() {
  const [rows, setRows] = useState<UserProfile[]>([])
  const [total, setTotal] = useState(0)
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState('')

  const [clientType, setClientType] = useState('')
  const [minRequests, setMinRequests] = useState(0)
  const [days, setDays] = useState(30)
  const [orderBy, setOrderBy] = useState<'total_tokens' | 'total_requests' | 'last_seen'>(
    'total_tokens',
  )
  const [offset, setOffset] = useState(0)

  async function load() {
    setLoading(true)
    setError('')
    try {
      const r = await api.users({
        client_type: clientType || undefined,
        min_requests: minRequests || undefined,
        days,
        order_by: orderBy,
        limit: PAGE_SIZE,
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
  }, [offset, clientType, minRequests, days, orderBy])

  return (
    <div className="p-6">
      <div className="flex items-center justify-between mb-6">
        <div>
          <h2 className="text-2xl font-semibold text-slate-900">用户画像</h2>
          <p className="text-sm text-slate-500 mt-1">
            按 user_id + 客户端类型聚合最近 {days} 天调用 (按 Token 消耗排序)
          </p>
        </div>
        <button
          onClick={() => void load()}
          disabled={loading}
          className="flex items-center gap-2 px-3 py-1.5 text-sm bg-white border border-slate-300 rounded-lg hover:bg-slate-50"
        >
          <RefreshCw className={`w-4 h-4 ${loading ? 'animate-spin' : ''}`} />
          刷新
        </button>
      </div>

      {/* 过滤栏 */}
      <div className="bg-white rounded-lg border border-slate-200 p-4 mb-4">
        <div className="grid grid-cols-1 md:grid-cols-4 gap-3">
          <ClientTypeFilter
            value={clientType}
            onChange={(v) => {
              setClientType(v)
              setOffset(0)
            }}
            className="px-3 py-2 text-sm border border-slate-300 rounded-md bg-white"
          />
          <select
            value={days}
            onChange={(e) => {
              setDays(Number(e.target.value))
              setOffset(0)
            }}
            className="px-3 py-2 text-sm border border-slate-300 rounded-md bg-white"
          >
            <option value={1}>近 1 天</option>
            <option value={7}>近 7 天</option>
            <option value={30}>近 30 天</option>
            <option value={90}>近 90 天</option>
          </select>
          <select
            value={orderBy}
            onChange={(e) => {
              setOrderBy(e.target.value as typeof orderBy)
              setOffset(0)
            }}
            className="px-3 py-2 text-sm border border-slate-300 rounded-md bg-white"
          >
            <option value="total_tokens">按 Token 消耗排序</option>
            <option value="total_requests">按请求数排序</option>
            <option value="last_seen">按最后活跃排序</option>
          </select>
          <input
            type="number"
            min={0}
            value={minRequests}
            onChange={(e) => {
              setMinRequests(Number(e.target.value) || 0)
              setOffset(0)
            }}
            placeholder="最少请求数"
            className="px-3 py-2 text-sm border border-slate-300 rounded-md"
          />
        </div>
      </div>

      {error && (
        <div className="mb-4 px-4 py-3 bg-red-50 border border-red-200 text-red-700 text-sm rounded-lg">
          {error}
        </div>
      )}

      {/* 表格 */}
      <div className="bg-white rounded-lg border border-slate-200 overflow-hidden">
        <div className="overflow-x-auto">
          <table className="w-full text-sm">
            <thead className="bg-slate-50 border-b border-slate-200">
              <tr className="text-left text-xs uppercase text-slate-500">
                <th className="px-4 py-3">用户</th>
                <th className="px-4 py-3">客户端</th>
                <th className="px-4 py-3">API Key</th>
                <th className="px-4 py-3 text-right">请求</th>
                <th className="px-4 py-3 text-right">错误</th>
                <th className="px-4 py-3 text-right">Token</th>
                <th className="px-4 py-3 text-right">平均延迟</th>
                <th className="px-4 py-3">主要模型</th>
                <th className="px-4 py-3">最后活跃</th>
                <th className="px-4 py-3 text-right">操作</th>
              </tr>
            </thead>
            <tbody className="divide-y divide-slate-100">
              {loading && rows.length === 0 ? (
                <tr>
                  <td colSpan={10} className="px-4 py-12 text-center text-slate-400">
                    加载中...
                  </td>
                </tr>
              ) : rows.length === 0 ? (
                <tr>
                  <td colSpan={10} className="px-4 py-12 text-center text-slate-400">
                    <Users className="w-6 h-6 mx-auto mb-2 opacity-50" />
                    没有匹配的用户
                  </td>
                </tr>
              ) : (
                rows.map((row) => (
                  <tr key={`${row.user_id}-${row.client_type}`} className="hover:bg-slate-50">
                    <td className="px-4 py-3 font-mono text-xs text-slate-700">
                      <Link
                        to={`/users/${encodeURIComponent(row.user_id)}`}
                        className="text-brand-600 hover:underline"
                        title={row.user_id}
                      >
                        {truncate(row.user_id, 20)}
                      </Link>
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
                    <td className="px-4 py-3 font-mono text-xs text-slate-500">
                      {row.api_key_prefix || '-'}
                    </td>
                    <td className="px-4 py-3 text-right text-slate-700">
                      {formatNumber(row.total_requests)}
                    </td>
                    <td className="px-4 py-3 text-right text-slate-700">
                      {row.total_errors > 0 ? (
                        <Badge variant="error">{formatNumber(row.total_errors)}</Badge>
                      ) : (
                        <span className="text-slate-400">0</span>
                      )}
                    </td>
                    <td className="px-4 py-3 text-right">
                      <div className="font-medium text-slate-700">
                        {formatNumber(row.total_tokens)}
                      </div>
                      <div className="text-xs text-slate-400">
                        {formatBytes(row.request_bytes || 0)} /{' '}
                        {formatBytes(row.response_bytes || 0)}
                      </div>
                    </td>
                    <td className="px-4 py-3 text-right text-slate-700">
                      {row.avg_duration_ms.toFixed(0)} ms
                    </td>
                    <td className="px-4 py-3">
                      <div className="flex flex-wrap gap-1 max-w-[12rem]">
                        {(row.models_used || []).slice(0, 3).map((m) => (
                          <span
                            key={m}
                            className="text-xs font-mono bg-slate-100 px-1.5 py-0.5 rounded text-slate-600"
                            title={m}
                          >
                            {truncate(m, 16)}
                          </span>
                        ))}
                        {(row.models_used || []).length > 3 && (
                          <span className="text-xs text-slate-400">
                            +{(row.models_used || []).length - 3}
                          </span>
                        )}
                      </div>
                      <div className="mt-1 flex gap-1">
                        {(row.providers_used || []).map((p) => (
                          <ProviderBadge key={p} provider={p} />
                        ))}
                      </div>
                    </td>
                    <td className="px-4 py-3 text-xs text-slate-500 whitespace-nowrap">
                      {formatTime(row.last_seen)}
                    </td>
                    <td className="px-4 py-3 text-right">
                      <div className="flex justify-end gap-1">
                        <Link
                          to={`/users/${encodeURIComponent(row.user_id)}`}
                          className="p-1.5 text-slate-500 hover:text-brand-600 hover:bg-slate-100 rounded"
                          title="查看时间线"
                        >
                          <Users className="w-4 h-4" />
                        </Link>
                        <button
                          onClick={() =>
                            void api.downloadExport(
                              { user_id: row.user_id, format: 'jsonl', max_rows: 100_000 },
                              `${row.user_id}-traces.jsonl`,
                            )
                          }
                          className="p-1.5 text-slate-500 hover:text-brand-600 hover:bg-slate-100 rounded"
                          title="导出该用户 JSONL"
                        >
                          <Download className="w-4 h-4" />
                        </button>
                      </div>
                    </td>
                  </tr>
                ))
              )}
            </tbody>
          </table>
        </div>

        {total > PAGE_SIZE && (
          <div className="flex items-center justify-between px-4 py-3 border-t border-slate-200 bg-slate-50">
            <div className="text-xs text-slate-500">
              共 {formatNumber(total)} 个用户 · 显示 {offset + 1}-
              {Math.min(offset + PAGE_SIZE, total)}
            </div>
            <div className="flex gap-2">
              <button
                onClick={() => setOffset(Math.max(0, offset - PAGE_SIZE))}
                disabled={offset === 0}
                className="px-3 py-1 text-xs border border-slate-300 rounded hover:bg-white disabled:opacity-50"
              >
                上一页
              </button>
              <button
                onClick={() => setOffset(offset + PAGE_SIZE)}
                disabled={offset + PAGE_SIZE >= total}
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
