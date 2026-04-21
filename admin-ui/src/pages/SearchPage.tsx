import { useState } from 'react'
import { Link } from 'react-router-dom'
import { Search, ExternalLink } from 'lucide-react'
import { api, type SearchHit } from '@/api/client'
import Badge, { ProviderBadge } from '@/components/Badge'
import ClientTypeFilter, { CLIENT_TYPE_LABEL } from '@/components/ClientTypeFilter'
import { formatTime, truncate } from '@/lib/utils'

export default function SearchPage() {
  const [query, setQuery] = useState('')
  const [field, setField] = useState<'request' | 'response'>('request')
  const [provider, setProvider] = useState('')
  const [clientType, setClientType] = useState('')
  const [userId, setUserId] = useState('')
  const [results, setResults] = useState<SearchHit[]>([])
  const [total, setTotal] = useState(0)
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState('')
  const [searched, setSearched] = useState(false)

  async function handleSearch(e?: React.FormEvent) {
    e?.preventDefault()
    if (query.length < 2) {
      setError('搜索词至少 2 个字符')
      return
    }
    setLoading(true)
    setError('')
    setSearched(true)
    try {
      const r = await api.search({
        q: query,
        field,
        provider: provider || undefined,
        client_type: clientType || undefined,
        user_id: userId || undefined,
      })
      setResults(r.items)
      setTotal(r.total)
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err))
    } finally {
      setLoading(false)
    }
  }

  return (
    <div className="p-6">
      <div className="mb-6">
        <h2 className="text-2xl font-semibold text-slate-900">全文搜索</h2>
        <p className="text-sm text-slate-500 mt-1">
          基于 ngram bloom filter 的请求/响应内容检索（前 2KB 预览文本）
        </p>
      </div>

      {/* Search form */}
      <form onSubmit={handleSearch} className="bg-white rounded-lg border border-slate-200 p-4 mb-4">
        <div className="flex flex-col sm:flex-row gap-3">
          <div className="flex-1 relative">
            <Search className="w-4 h-4 absolute left-3 top-1/2 -translate-y-1/2 text-slate-400" />
            <input
              type="text"
              value={query}
              onChange={(e) => setQuery(e.target.value)}
              placeholder="输入搜索关键词 (建议 >= 4 字符以提高准确率)..."
              className="w-full pl-9 pr-3 py-2 text-sm border border-slate-300 rounded-md focus:outline-none focus:ring-2 focus:ring-brand-500 focus:border-transparent"
            />
          </div>
          <select
            value={field}
            onChange={(e) => setField(e.target.value as 'request' | 'response')}
            className="px-3 py-2 text-sm border border-slate-300 rounded-md bg-white"
          >
            <option value="request">请求内容</option>
            <option value="response">响应内容</option>
          </select>
          <ClientTypeFilter
            value={clientType}
            onChange={setClientType}
            className="px-3 py-2 text-sm border border-slate-300 rounded-md bg-white w-40"
          />
          <input
            type="text"
            value={provider}
            onChange={(e) => setProvider(e.target.value)}
            placeholder="Provider (可选)"
            className="px-3 py-2 text-sm border border-slate-300 rounded-md w-32"
          />
          <input
            type="text"
            value={userId}
            onChange={(e) => setUserId(e.target.value)}
            placeholder="user_id (可选)"
            className="px-3 py-2 text-sm border border-slate-300 rounded-md w-32"
          />
          <button
            type="submit"
            disabled={loading || query.length < 2}
            className="px-4 py-2 text-sm bg-brand-500 text-white rounded-md hover:bg-brand-600 disabled:opacity-50 whitespace-nowrap"
          >
            {loading ? '搜索中...' : '搜索'}
          </button>
        </div>
      </form>

      {error && (
        <div className="mb-4 px-4 py-3 bg-red-50 border border-red-200 text-red-700 text-sm rounded-lg">
          {error}
        </div>
      )}

      {/* Results */}
      {searched && !loading && (
        <div className="bg-white rounded-lg border border-slate-200 overflow-hidden">
          <div className="px-4 py-3 bg-slate-50 border-b border-slate-200 flex items-center justify-between">
            <span className="text-sm text-slate-700">
              找到 <strong>{total}</strong> 条结果
            </span>
            <Badge variant="gray">{field === 'request' ? '请求' : '响应'}内容</Badge>
          </div>

          {results.length === 0 ? (
            <div className="p-8 text-center text-slate-400 text-sm">
              <Search className="w-8 h-8 mx-auto mb-2 opacity-50" />
              没有找到匹配的记录
            </div>
          ) : (
            <div className="divide-y divide-slate-100">
              {results.map((hit) => (
                <Link
                  key={hit.request_id}
                  to={`/traces/${hit.request_id}`}
                  className="block px-4 py-3 hover:bg-slate-50 transition-colors"
                >
                  <div className="flex items-center gap-2 mb-1.5">
                    <ProviderBadge provider={hit.provider} />
                    {hit.client_type && (
                      <Badge variant={hit.client_type === 'claude_code' ? 'purple' : 'gray'}>
                        {CLIENT_TYPE_LABEL[hit.client_type] || hit.client_type}
                      </Badge>
                    )}
                    <span className="text-xs font-mono text-slate-600">{hit.model}</span>
                    {hit.is_error && <Badge variant="error">error</Badge>}
                    <span className="text-xs text-slate-400 ml-auto">{formatTime(hit.created_at)}</span>
                    <ExternalLink className="w-3 h-3 text-slate-400" />
                  </div>
                  <div className="text-xs text-slate-500 mb-1">
                    user: {hit.user_id || '-'}
                  </div>
                  <pre className="text-xs bg-slate-50 rounded p-2 whitespace-pre-wrap font-mono text-slate-700 max-h-24 overflow-hidden">
                    {highlightSnippet(hit.snippet, query)}
                  </pre>
                </Link>
              ))}
            </div>
          )}
        </div>
      )}
    </div>
  )
}

function highlightSnippet(snippet: string, query: string): string {
  // 简单高亮: 返回文本 (实际高亮需要 dangerouslySetInnerHTML, 这里只截断展示)
  return truncate(snippet, 500)
}
