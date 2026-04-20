import { useEffect, useState } from 'react'
import { TrendingUp } from 'lucide-react'
import { api, type UsageStatsResponse } from '@/api/client'
import { ProviderBadge } from '@/components/Badge'
import { formatBytes, formatNumber } from '@/lib/utils'

export default function StatsPage() {
  const [data, setData] = useState<UsageStatsResponse | null>(null)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState('')
  const [groupBy, setGroupBy] = useState<'day' | 'hour'>('day')

  async function load() {
    setLoading(true)
    setError('')
    try {
      const r = await api.usage({ group_by: groupBy })
      setData(r)
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e))
    } finally {
      setLoading(false)
    }
  }

  useEffect(() => {
    void load()
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [groupBy])

  const totals = data?.items.reduce(
    (acc, r) => ({
      requests: acc.requests + r.request_count,
      errors: acc.errors + r.error_count,
      tokens: acc.tokens + r.total_tokens,
      bytes: acc.bytes + (r.request_bytes || 0) + (r.response_bytes || 0),
    }),
    { requests: 0, errors: 0, tokens: 0, bytes: 0 },
  )

  return (
    <div className="p-6">
      <div className="flex items-center justify-between mb-6">
        <div>
          <h2 className="text-2xl font-semibold text-slate-900">用量统计</h2>
          <p className="text-sm text-slate-500 mt-1">按时间桶 × provider × model 聚合</p>
        </div>
        <div className="flex gap-1 border border-slate-300 rounded-md overflow-hidden bg-white">
          {(['hour', 'day'] as const).map((v) => (
            <button
              key={v}
              onClick={() => setGroupBy(v)}
              className={`px-3 py-1.5 text-sm ${
                groupBy === v
                  ? 'bg-brand-500 text-white'
                  : 'text-slate-700 hover:bg-slate-50'
              }`}
            >
              按{v === 'hour' ? '小时' : '日'}
            </button>
          ))}
        </div>
      </div>

      {/* 总览卡片 */}
      <div className="grid grid-cols-2 md:grid-cols-4 gap-4 mb-6">
        <KPI label="请求数" value={formatNumber(totals?.requests ?? 0)} />
        <KPI
          label="错误数"
          value={formatNumber(totals?.errors ?? 0)}
          delta={
            totals && totals.requests > 0
              ? `${((totals.errors / totals.requests) * 100).toFixed(2)}%`
              : undefined
          }
        />
        <KPI label="总 tokens" value={formatNumber(totals?.tokens ?? 0)} />
        <KPI label="总流量" value={formatBytes(totals?.bytes ?? 0)} />
      </div>

      {error && (
        <div className="mb-4 px-4 py-3 bg-red-50 border border-red-200 text-red-700 text-sm rounded-lg">
          {error}
        </div>
      )}

      <div className="bg-white rounded-lg border border-slate-200 overflow-hidden">
        <div className="overflow-x-auto">
          <table className="w-full text-sm">
            <thead className="bg-slate-50 border-b border-slate-200">
              <tr className="text-left text-xs uppercase text-slate-500">
                <th className="px-4 py-3">时间桶</th>
                <th className="px-4 py-3">Provider</th>
                <th className="px-4 py-3">Model</th>
                <th className="px-4 py-3 text-right">请求</th>
                <th className="px-4 py-3 text-right">错误</th>
                <th className="px-4 py-3 text-right">Prompt</th>
                <th className="px-4 py-3 text-right">Completion</th>
                <th className="px-4 py-3 text-right">Total</th>
                <th className="px-4 py-3 text-right">平均延迟</th>
              </tr>
            </thead>
            <tbody className="divide-y divide-slate-100">
              {loading && (
                <tr>
                  <td colSpan={9} className="px-4 py-8 text-center text-slate-400">
                    加载中...
                  </td>
                </tr>
              )}
              {!loading &&
                data?.items.map((row, i) => (
                  <tr key={`${row.bucket}-${row.provider}-${row.model}-${i}`} className="hover:bg-slate-50">
                    <td className="px-4 py-3 text-slate-600 font-mono text-xs">{row.bucket}</td>
                    <td className="px-4 py-3">
                      <ProviderBadge provider={row.provider} />
                    </td>
                    <td className="px-4 py-3 font-mono text-xs text-slate-700">{row.model}</td>
                    <td className="px-4 py-3 text-right text-slate-700">
                      {formatNumber(row.request_count)}
                    </td>
                    <td className="px-4 py-3 text-right">
                      <span className={row.error_count > 0 ? 'text-red-600' : 'text-slate-400'}>
                        {formatNumber(row.error_count)}
                      </span>
                    </td>
                    <td className="px-4 py-3 text-right text-slate-700">
                      {formatNumber(row.prompt_tokens)}
                    </td>
                    <td className="px-4 py-3 text-right text-slate-700">
                      {formatNumber(row.completion_tokens)}
                    </td>
                    <td className="px-4 py-3 text-right text-slate-900 font-medium">
                      {formatNumber(row.total_tokens)}
                    </td>
                    <td className="px-4 py-3 text-right text-slate-600">
                      {row.avg_latency_ms.toFixed(0)}ms
                    </td>
                  </tr>
                ))}
              {!loading && data?.items.length === 0 && (
                <tr>
                  <td colSpan={9} className="px-4 py-8 text-center text-slate-400">
                    <TrendingUp className="w-6 h-6 mx-auto mb-2 opacity-50" />
                    暂无数据
                  </td>
                </tr>
              )}
            </tbody>
          </table>
        </div>
      </div>
    </div>
  )
}

function KPI({ label, value, delta }: { label: string; value: string; delta?: string }) {
  return (
    <div className="bg-white border border-slate-200 rounded-lg p-4">
      <div className="text-xs text-slate-500">{label}</div>
      <div className="text-2xl font-semibold text-slate-900 mt-1">{value}</div>
      {delta && <div className="text-xs text-slate-500 mt-1">{delta}</div>}
    </div>
  )
}
