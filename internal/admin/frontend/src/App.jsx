import { useState, useEffect } from 'react'
import Sidebar from './components/Sidebar'
import Dashboard from './components/Dashboard'
import ApiKeys from './components/ApiKeys'
import Tenants from './components/Tenants'
import Usage from './components/Usage'
import Login from './components/Login'
import OpenAI from './components/providers/OpenAI'
import Anthropic from './components/providers/Anthropic'
import Gemini from './components/providers/Gemini'

function App() {
  // Auth state
  const getStoredToken = () => localStorage.getItem('auth_token')
  const [authChecking, setAuthChecking] = useState(!getStoredToken())
  const [isAuthenticated, setIsAuthenticated] = useState(!!getStoredToken())
  const [needsSetup, setNeedsSetup] = useState(false)

  // App state - initialize from URL hash
  const getViewFromHash = () => {
    const hash = window.location.hash.slice(1)
    return hash || 'dashboard'
  }
  const [activeView, setActiveView] = useState(getViewFromHash)
  const [info, setInfo] = useState(null)
  const [stats, setStats] = useState(null)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState(null)

  // Sync URL hash with active view
  useEffect(() => {
    const newHash = activeView === 'dashboard' ? '' : activeView
    if (window.location.hash.slice(1) !== newHash) {
      window.location.hash = newHash
    }
  }, [activeView])

  // Handle browser back/forward navigation
  useEffect(() => {
    const handleHashChange = () => {
      const view = getViewFromHash()
      if (view !== activeView) {
        setActiveView(view)
      }
    }
    window.addEventListener('hashchange', handleHashChange)
    return () => window.removeEventListener('hashchange', handleHashChange)
  }, [activeView])

  // Check authentication if no token exists
  useEffect(() => {
    if (authChecking) {
      checkAuth()
    }
  }, [authChecking])

  // Fetch data when authenticated
  useEffect(() => {
    if (isAuthenticated) {
      fetchAllData()
      const interval = setInterval(fetchAllData, 30000)
      return () => clearInterval(interval)
    }
  }, [isAuthenticated])

  const checkAuth = async () => {
    const maxRetries = 10
    const baseDelay = 500

    for (let attempt = 0; attempt < maxRetries; attempt++) {
      try {
        const statusRes = await fetch('/api/auth/status', { credentials: 'include' })

        if (!statusRes.ok) {
          if (attempt < maxRetries - 1) {
            await new Promise(r => setTimeout(r, baseDelay * Math.pow(2, attempt)))
            continue
          }
          setAuthChecking(false)
          return
        }

        const status = await statusRes.json()

        if (!status.password_set) {
          setNeedsSetup(true)
        }
        setAuthChecking(false)
        return

      } catch (err) {
        if (attempt < maxRetries - 1) {
          await new Promise(r => setTimeout(r, baseDelay * Math.pow(2, attempt)))
          continue
        }
        console.error('Auth check failed after retries:', err)
      }
    }

    setAuthChecking(false)
  }

  const handleLogin = (token) => {
    if (token) {
      localStorage.setItem('auth_token', token)
    }
    setIsAuthenticated(true)
    setNeedsSetup(false)
  }

  const handleLogout = async () => {
    try {
      await fetch('/api/auth/logout', {
        method: 'POST',
        credentials: 'include'
      })
    } catch (err) {
      console.error('Logout error:', err)
    }
    localStorage.removeItem('auth_token')
    setIsAuthenticated(false)
  }

  const getAuthHeaders = () => {
    const token = localStorage.getItem('auth_token')
    return token ? { 'Authorization': `Bearer ${token}` } : {}
  }

  const fetchAllData = async () => {
    try {
      setLoading(true)
      setError(null)

      const headers = getAuthHeaders()
      const fetchOptions = { credentials: 'include', headers }

      const [infoRes, statsRes] = await Promise.all([
        fetch('/api/info', fetchOptions),
        fetch('/api/stats', fetchOptions)
      ])

      if (infoRes.status === 401) {
        handleLogout()
        return
      }

      if (infoRes.ok) {
        setInfo(await infoRes.json())
      }
      if (statsRes.ok) {
        setStats(await statsRes.json())
      }

    } catch (err) {
      console.error('Fetch error:', err)
      setError('Failed to fetch data')
    } finally {
      setLoading(false)
    }
  }

  const getTitle = () => {
    const titles = {
      'dashboard': 'Dashboard',
      'api-keys': 'API Keys',
      'tenants': 'Tenants',
      'usage': 'Usage',
      'openai': 'OpenAI',
      'anthropic': 'Anthropic',
      'gemini': 'Gemini'
    }
    return titles[activeView] || 'AIBox Admin'
  }

  const renderContent = () => {
    switch (activeView) {
      case 'dashboard':
        return <Dashboard info={info} stats={stats} loading={loading} />
      case 'api-keys':
        return <ApiKeys getAuthHeaders={getAuthHeaders} onLogout={handleLogout} />
      case 'tenants':
        return <Tenants getAuthHeaders={getAuthHeaders} onLogout={handleLogout} />
      case 'usage':
        return <Usage getAuthHeaders={getAuthHeaders} onLogout={handleLogout} />
      case 'openai':
        return <OpenAI getAuthHeaders={getAuthHeaders} onLogout={handleLogout} />
      case 'anthropic':
        return <Anthropic getAuthHeaders={getAuthHeaders} onLogout={handleLogout} />
      case 'gemini':
        return <Gemini getAuthHeaders={getAuthHeaders} onLogout={handleLogout} />
      default:
        return <Dashboard info={info} stats={stats} loading={loading} />
    }
  }

  // Loading state
  if (authChecking) {
    return (
      <div className="min-h-screen flex items-center justify-center bg-gray-100">
        <div className="text-center">
          <div className="animate-spin rounded-full h-12 w-12 border-b-2 border-blue-600 mx-auto"></div>
          <p className="mt-4 text-gray-600">Connecting to AIBox...</p>
        </div>
      </div>
    )
  }

  // Auth required
  if (!isAuthenticated) {
    return <Login onLogin={handleLogin} isSetup={needsSetup} />
  }

  return (
    <div className="flex h-screen bg-gray-50">
      <Sidebar
        activeView={activeView}
        onNavigate={setActiveView}
        version={info?.version}
      />
      <div className="flex-1 flex flex-col overflow-hidden">
        {/* Header */}
        <header className="bg-white border-b border-gray-200 px-6 py-4 flex items-center justify-between">
          <h1 className="text-xl font-semibold text-gray-900">{getTitle()}</h1>
          <div className="flex items-center gap-4">
            <button
              onClick={fetchAllData}
              className="text-sm text-gray-600 hover:text-gray-900 flex items-center gap-1"
              title="Refresh data"
            >
              <svg className={`w-4 h-4 ${loading ? 'animate-spin' : ''}`} fill="none" stroke="currentColor" viewBox="0 0 24 24">
                <path strokeLinecap="round" strokeLinejoin="round" strokeWidth="2" d="M4 4v5h.582m15.356 2A8.001 8.001 0 004.582 9m0 0H9m11 11v-5h-.581m0 0a8.003 8.003 0 01-15.357-2m15.357 2H15" />
              </svg>
              Refresh
            </button>
            <button
              onClick={handleLogout}
              className="text-sm text-gray-600 hover:text-gray-900"
            >
              Logout
            </button>
          </div>
        </header>

        {/* Main content */}
        <main className="flex-1 overflow-auto p-6">
          {error && (
            <div className="bg-red-50 border border-red-200 rounded-lg p-4 mb-4">
              <p className="text-sm text-red-700">{error}</p>
            </div>
          )}
          {renderContent()}
        </main>
      </div>
    </div>
  )
}

export default App
