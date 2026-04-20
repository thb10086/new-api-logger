import { useEffect, useState } from 'react'
import { Activity, AlertTriangle, CheckCircle2 } from 'lucide-react'
import { api, type HealthStatsResponse } from '@/api/client'
import { ProviderBadge } from '@/components/Badge'
import { formatNumber } from '@/lib/utils'

export default function HealthPage() {
  const [data, setData] = useState<HealthStatsResponse | null>(null)
  const [error, setError] = useState('')
  const [windowHours, setWindowHours] = useState(1)

  async function load() {
    try {
      const r = await api.health(windowHours)
      setData(r)
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e))
    }
  }

  useEffect(() => {
    void load()
    const id = setInterval(load, 30_000) // 30s 自动刷新
    return () => clearInterval(id)
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [windowHours])

  return (
    <div className="p-6">
      <div className="flex items-center justify-between mb-6">
        <div>
          <h2 className="text-2xl font-semibold text-slate-900">实时健康</h2>
          <p className="text-sm text-slate-500 mt-1">每 30 秒自动刷新</p>
        </div>
        <select
          value={windowHours}
          onChange={(e) => setWindowHours(Number(e.target.value))}
          className="px-3 py-1.5 text-sm border border-slate-300 rounded-md bg-white"
        >
          <option value={1}>近 1 小时</option>
          <option value={6}>近 6 小时</option>
          <option value={12}>近 12 小时</option>
          <option value={24}>近 24 小时</option>
        </select>
      </div>

      {error && (
        <div className="mb-4 px-4 py-3 bg-red-50 border border-red-200 text-red-700 text-sm rounded-lg">
          {error}
        </div>
      )}

      <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
        {data?.items.map((p) => {
          const healthy = p.error_rate_pct < 1.0
          return (
            <div
              key={p.provider}
              className="bg-white border border-slate-200 rounded-lg p-5"
            >
              <div className="flex items-start justify-between mb-3">
                <div>
                  <ProviderBadge provider={p.provider} />
                  <div className="mt-2 text-2xl font-semibold text-slate-900">
                    {formatNumber(p.requests)}
                    <span className="text-sm font-normal text-slate-500 ml-1">requests</span>
                  </div>
                </div>
                {healthy ? (
                  <div className="text-emerald-500">
                    <CheckCircle2 className="w-7 h-7" />
                  </div>
                ) : (
                  <div className="text-red-500">
                    <AlertTriangle className="w-7 h-7" />
                  </div>
                )}
              </div>

              <div className="grid grid-cols-2 gap-3 pt-3 border-t border-slate-100">
                <Mini label="错误率" value={`${p.error_rate_pct.toFixed(2)}%`} />
                <Mini label="错误数" value={formatNumber(p.errors)} />
                <Mini label="平均延迟" value={`${p.avg_latency_ms.toFixed(0)}ms`} />
                <Mini
                  label="平均 TTFT"
                  value={p.avg_ttft_ms ? `${p.avg_ttft_ms.toFixed(0)}ms` : '-'}
                />
                <Mini label="Tokens" value={formatNumber(p.tokens)} />
              </div>
            </div>
          )
        })}

        {data?.items.length === 0 && (
          <div className="md:col-span-2 bg-white border border-slate-200 rounded-lg p-8 text-center text-slate-400">
            <Activity className="w-8 h-8 mx-auto mb-2 opacity-50" />
            最近 {windowHours} 小时内没有调用记录
          </div>
        )}
      </div>
    </div>
  )
}

function Mini({ label, value }: { label: string; value: string }) {
  return (
    <div>
      <div className="text-xs text-slate-500">{label}</div>
      <div className="text-sm font-medium text-slate-800 mt-0.5">{value}</div>
    </div>
  )
}
