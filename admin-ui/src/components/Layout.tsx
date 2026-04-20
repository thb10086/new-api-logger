import { Link, NavLink, Outlet, useNavigate } from 'react-router-dom'
import { Activity, AlertTriangle, BarChart3, ListTree, LogOut, Search } from 'lucide-react'
import { clearToken } from '@/api/client'
import { cn } from '@/lib/utils'

const navItems = [
  { to: '/traces', label: 'Traces', icon: ListTree },
  { to: '/search', label: '全文搜索', icon: Search },
  { to: '/errors', label: '错误诊断', icon: AlertTriangle },
  { to: '/stats', label: '统计看板', icon: BarChart3 },
  { to: '/health', label: '实时健康', icon: Activity },
]

export default function Layout() {
  const navigate = useNavigate()

  function handleLogout() {
    clearToken()
    navigate('/')
    window.location.reload()
  }

  return (
    <div className="min-h-screen flex">
      {/* Sidebar */}
      <aside className="w-56 bg-slate-900 text-slate-100 flex flex-col">
        <div className="px-5 py-5 border-b border-slate-800">
          <Link to="/" className="block">
            <h1 className="text-lg font-semibold">LLM Logger</h1>
            <p className="text-xs text-slate-400 mt-0.5">Admin Console</p>
          </Link>
        </div>

        <nav className="flex-1 py-4">
          {navItems.map((item) => (
            <NavLink
              key={item.to}
              to={item.to}
              className={({ isActive }) =>
                cn(
                  'flex items-center gap-3 px-5 py-2.5 text-sm transition-colors',
                  isActive
                    ? 'bg-brand-600/20 text-brand-100 border-l-2 border-brand-500'
                    : 'text-slate-300 hover:bg-slate-800 border-l-2 border-transparent',
                )
              }
            >
              <item.icon className="w-4 h-4" />
              {item.label}
            </NavLink>
          ))}
        </nav>

        <div className="p-3 border-t border-slate-800">
          <button
            onClick={handleLogout}
            className="w-full flex items-center gap-2 px-3 py-2 text-sm text-slate-300 hover:bg-slate-800 rounded-md transition-colors"
          >
            <LogOut className="w-4 h-4" />
            退出
          </button>
        </div>
      </aside>

      {/* Main content */}
      <main className="flex-1 overflow-auto">
        <Outlet />
      </main>
    </div>
  )
}
