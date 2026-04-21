import { useState } from 'react'
import { Navigate, Route, Routes } from 'react-router-dom'
import Layout from '@/components/Layout'
import TokenGate from '@/components/TokenGate'
import TracesPage from '@/pages/TracesPage'
import TraceDetailPage from '@/pages/TraceDetailPage'
import StatsPage from '@/pages/StatsPage'
import HealthPage from '@/pages/HealthPage'
import SearchPage from '@/pages/SearchPage'
import ErrorsPage from '@/pages/ErrorsPage'
import UsersPage from '@/pages/UsersPage'
import UserDetailPage from '@/pages/UserDetailPage'
import { getToken } from '@/api/client'

export default function App() {
  const [authed, setAuthed] = useState<boolean>(!!getToken())

  if (!authed) {
    return <TokenGate onAuthed={() => setAuthed(true)} />
  }

  return (
    <Routes>
      <Route element={<Layout />}>
        <Route path="/" element={<Navigate to="/traces" replace />} />
        <Route path="/traces" element={<TracesPage />} />
        <Route path="/traces/:requestId" element={<TraceDetailPage />} />
        <Route path="/users" element={<UsersPage />} />
        <Route path="/users/:userId" element={<UserDetailPage />} />
        <Route path="/search" element={<SearchPage />} />
        <Route path="/errors" element={<ErrorsPage />} />
        <Route path="/stats" element={<StatsPage />} />
        <Route path="/health" element={<HealthPage />} />
        <Route path="*" element={<Navigate to="/traces" replace />} />
      </Route>
    </Routes>
  )
}
