import { useEffect, useState } from 'react'
import { Link, useParams } from 'react-router-dom'
import { ArrowLeft, Download, ExternalLink, RefreshCw } from 'lucide-react'
import { api, type TraceSummary, type UserProfile } from '@/api/client'
import Badge, { ProviderBadge, StatusBadge } from '@/components/Badge'
import { CLIENT_TYPE_LABEL } from '@/components/ClientTypeFilter'
import { formatBytes, formatDuration, formatNumber, formatTime, truncate } from '@/lib/utils'

const PAGE_SIZE = 50

export default function UserDetailPage() {
  const { userId = '' } = useParams<{ userId: string }>()
  const [profile, setProfile] = useState<UserProfile | null>(null)
  const [traces, setTraces] = useState<TraceSummary[]>([])
  const [total, setTotal] = useState(0)
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState('')
  const [days, setDays] = useState(30)
  const [offset, setOffset] = useState(0)
  const [exporting, setExporting] = useState(false)

  async function load() {
    setLoading(true)
    setError('')
    try {
      const [p, t] = await Promise.all([
        api.userProfile(userId, days).catch(() => null),
        api.userTraces(userId, { limit: PAGE_SIZE, offset, days }),
      ])
      setProfile(p)
      setTraces(t.items)
      setTotal(t.total)
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e))
    } finally {
      setLoading(false)
    }
  }

  useEffect(() => {
    void load()
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [userId, days, offset])

  async function handleExport(format: 'jsonl' | 'csv', includeBody: boolean) {
    setExporting(true)
    setError('')
    try {
      await api.downloadExport(
        {
          user_id: userId,
          format,
          include_body: includeBody,
          max_rows: 100_000,
        },
        `${userId}-traces-${new Date().toISOString().slice(0, 10)}.${format}`,
      )
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e))
    } finally {
      setExporting(false)
    }
  }

  return (
    <div className="p-6 max-w-7xl">
      <Link
        to="/users"
        className="inline-flex items-center gap-1 text-sm text-slate-600 hover:text-slate-900 mb-4"
      >
        <ArrowLeft className="w-4 h-4" />
        返回用户画像
      </Link>

      <div className="flex items-start justify-between mb-6">
        <div>
          <h2 className="text-2xl font-semibold text-slate-900 font-mono">{userId}</h2>
          <p className="text-sm text-slate-500 mt-1">用户完整调用历史 · 最近 {days} 天</p>
        </div>
        <div className="flex items-center gap-2">
          <select
            value={days}
            onChange={(e) => {
              setDays(Number(e.target.value))
              setOffset(0)
            }}
            className="px-3 py-1.5 text-sm border border-slate-300 rounded-md bg-white"
          >
            <option value={1}>近 1 天</option>
            <option value={7}>近 7 天</option>
            <option value={30}>近 30 天</option>
            <option value={90}>近 90 天</option>
          </select>
          <button
            onClick={() => void handleExport('jsonl', false)}
            disabled={exporting}
            className="flex items-center gap-2 px-3 py-1.5 text-sm bg-white border border-slate-300 rounded-lg hover:bg-slate-50 disabled:opacity-50"
          >
            <Download className="w-4 h-4" />
            导出元数据
          </button>
          <button
            onClick={() => void handleExport('jsonl', true)}
            disabled={exporting}
            className="flex items-center gap-2 px-3 py-1.5 text-sm bg-brand-500 text-white rounded-lg hover:bg-brand-600 disabled:opacity-50"
          >
            <Download className="w-4 h-4" />
            导出完整对话
          </button>
          <button
            onClick={() => void load()}
            disabled={loading}
            className="flex items-center gap-2 px-3 py-1.5 text-sm bg-white border border-slate-300 rounded-lg hover:bg-slate-50"
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

      {/* 画像 stats */}
      {profile && (
        <div className="grid grid-cols-2 md:grid-cols-4 gap-3 mb-6">
          <StatCard label="请求总数" value={formatNumber(profile.total_requests)} />
          <StatCard
            label="Token 消耗"
            value={formatNumber(profile.total_tokens)}
            sub={`prompt ${formatNumber(profile.prompt_tokens)} / completion ${formatNumber(profile.completion_tokens)}`}
          />
          <StatCard
            label="错误数"
            value={formatNumber(profile.total_errors)}
            sub={`错误率 ${((profile.total_errors / Math.max(profile.total_requests, 1)) * 100).toFixed(2)}%`}
          />
          <StatCard
            label="平均延迟"
            value={`${profile.avg_duration_ms.toFixed(0)} ms`}
            sub={`流量 ${formatBytes((profile.request_bytes || 0) + (profile.response_bytes || 0))}`}
          />
        </div>
      )}

      {profile && (
        <div className="bg-white rounded-lg border border-slate-200 p-4 mb-4 space-y-2">
          <div className="flex items-center gap-2 flex-wrap">
            <span className="text-xs text-slate-500">客户端:</span>
            {profile.client_type ? (
              <Badge variant={profile.client_type === 'claude_code' ? 'purple' : 'gray'}>
                {CLIENT_TYPE_LABEL[profile.client_type] || profile.client_type}
              </Badge>
            ) : (
              <span className="text-xs text-slate-400">未知</span>
            )}
            <span className="text-xs text-slate-400 ml-4">
              UA: <span className="font-mono">{truncate(profile.user_agent || '-', 80)}</span>
            </span>
          </div>
          <div className="flex items-center gap-2 flex-wrap">
            <span className="text-xs text-slate-500">API Key:</span>
            <span className="text-xs font-mono text-slate-700">{profile.api_key_prefix || '-'}</span>
          </div>
          <div className="flex items-center gap-2 flex-wrap">
            <span className="text-xs text-slate-500">使用模型:</span>
            {(profile.models_used || []).map((m) => (
              <span
                key={m}
                className="text-xs font-mono bg-slate-100 px-2 py-0.5 rounded text-slate-700"
              >
                {m}
              </span>
            ))}
          </div>
          <div className="flex items-center gap-2 flex-wrap">
            <span className="text-xs text-slate-500">Provider:</span>
            {(profile.providers_used || []).map((p) => (
              <span key={p}>
                <ProviderBadge provider={p} />
              </span>
            ))}
          </div>
          <div className="flex items-center gap-4 text-xs text-slate-500">
            <span>首次见: {formatTime(profile.first_seen)}</span>
            <span>最后活跃: {formatTime(profile.last_seen)}</span>
          </div>
        </div>
      )}

      {/* 时间线 */}
      <div className="bg-white rounded-lg border border-slate-200 overflow-hidden">
        <div className="px-4 py-3 bg-slate-50 border-b border-slate-200 text-sm text-slate-700">
          调用时间线 ({formatNumber(total)} 条)
        </div>
        <div className="overflow-x-auto">
          <table className="w-full text-sm">
            <thead className="bg-slate-50 border-b border-slate-200">
              <tr className="text-left text-xs uppercase text-slate-500">
                <th className="px-4 py-3">时间</th>
                <th className="px-4 py-3">Provider</th>
                <th className="px-4 py-3">Model</th>
                <th className="px-4 py-3">Status</th>
                <th className="px-4 py-3 text-right">Tokens</th>
                <th className="px-4 py-3 text-right">耗时</th>
                <th className="px-4 py-3">Preview</th>
                <th className="px-4 py-3" />
              </tr>
            </thead>
            <tbody className="divide-y divide-slate-100">
              {traces.length === 0 ? (
                <tr>
                  <td colSpan={8} className="px-4 py-12 text-center text-slate-400">
                    没有记录
                  </td>
                </tr>
              ) : (
                traces.map((row) => (
                  <tr key={row.request_id} className="hover:bg-slate-50">
                    <td className="px-4 py-3 font-mono text-xs text-slate-600 whitespace-nowrap">
                      {formatTime(row.created_at)}
                    </td>
                    <td className="px-4 py-3">
                      <ProviderBadge provider={row.provider} />
                      {row.is_stream && (
                        <Badge variant="gray" className="ml-1">
                          SSE
                        </Badge>
                      )}
                    </td>
                    <td className="px-4 py-3 font-mono text-xs text-slate-700">{row.model}</td>
                    <td className="px-4 py-3">
                      <StatusBadge status={row.upstream_status} />
                      {row.is_error && (
                        <Badge variant="error" className="ml-1">
                          ERR
                        </Badge>
                      )}
                    </td>
                    <td className="px-4 py-3 text-right text-slate-700">
                      {formatNumber(row.total_tokens)}
                    </td>
                    <td className="px-4 py-3 text-right text-slate-700">
                      {formatDuration(row.duration_ms)}
                    </td>
                    <td className="px-4 py-3 text-slate-600 max-w-sm">
                      <div className="truncate" title={row.request_text_preview || ''}>
                        {truncate(row.request_text_preview, 80) || '-'}
                      </div>
                    </td>
                    <td className="px-4 py-3 text-right">
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
        </div>

        {total > PAGE_SIZE && (
          <div className="flex items-center justify-between px-4 py-3 border-t border-slate-200 bg-slate-50">
            <div className="text-xs text-slate-500">
              显示 {offset + 1}-{Math.min(offset + PAGE_SIZE, total)} / {total}
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

function StatCard({ label, value, sub }: { label: string; value: string; sub?: string }) {
  return (
    <div className="bg-white rounded-lg border border-slate-200 p-4">
      <div className="text-xs text-slate-500">{label}</div>
      <div className="text-2xl font-semibold text-slate-900 mt-1">{value}</div>
      {sub && <div className="text-xs text-slate-400 mt-1">{sub}</div>}
    </div>
  )
}
