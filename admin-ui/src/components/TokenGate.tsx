import { FormEvent, useState } from 'react'
import { KeyRound, Loader2 } from 'lucide-react'
import { setToken, clearToken, api } from '@/api/client'

interface Props {
  onAuthed: () => void
}

export default function TokenGate({ onAuthed }: Props) {
  const [token, setTokenInput] = useState('')
  const [error, setError] = useState<string>('')
  const [loading, setLoading] = useState(false)

  async function handleSubmit(e: FormEvent) {
    e.preventDefault()
    if (!token.trim()) {
      setError('请输入 Token')
      return
    }
    setLoading(true)
    setError('')
    // 先保存 token, 再通过 ping 验证是否有效
    setToken(token.trim())
    try {
      await api.verifyToken()
      onAuthed()
    } catch {
      clearToken()
      setError('Token 无效或服务不可用, 请检查后重试')
    } finally {
      setLoading(false)
    }
  }

  return (
    <div className="min-h-screen flex items-center justify-center bg-gradient-to-br from-slate-50 to-slate-200">
      <div className="w-full max-w-md bg-white rounded-2xl shadow-xl p-8">
        <div className="flex items-center gap-3 mb-6">
          <div className="w-12 h-12 rounded-xl bg-brand-500 text-white flex items-center justify-center">
            <KeyRound className="w-6 h-6" />
          </div>
          <div>
            <h1 className="text-xl font-semibold text-slate-900">LLM Logger</h1>
            <p className="text-sm text-slate-500">Admin Console</p>
          </div>
        </div>

        <form onSubmit={handleSubmit} className="space-y-4">
          <div>
            <label className="block text-sm font-medium text-slate-700 mb-1">API Token</label>
            <input
              type="password"
              value={token}
              onChange={(e) => setTokenInput(e.target.value)}
              placeholder="LLMLOG_API_TOKEN"
              autoFocus
              className="w-full px-3 py-2 border border-slate-300 rounded-lg focus:outline-none focus:ring-2 focus:ring-brand-500 focus:border-transparent font-mono text-sm"
            />
            <p className="mt-1 text-xs text-slate-500">
              对应 query-api 的环境变量 <code className="text-brand-600">LLMLOG_API_TOKEN</code>
            </p>
          </div>

          {error && (
            <div className="text-sm text-red-600 bg-red-50 px-3 py-2 rounded-lg">{error}</div>
          )}

          <button
            type="submit"
            disabled={loading}
            className="w-full py-2.5 bg-brand-500 text-white font-medium rounded-lg hover:bg-brand-600 transition-colors disabled:opacity-60 flex items-center justify-center gap-2"
          >
            {loading && <Loader2 className="w-4 h-4 animate-spin" />}
            {loading ? '验证中...' : '进入控制台'}
          </button>
        </form>
      </div>
    </div>
  )
}
