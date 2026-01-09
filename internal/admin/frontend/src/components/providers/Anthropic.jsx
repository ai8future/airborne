import { useState, useEffect } from 'react'

function Anthropic({ getAuthHeaders, onLogout }) {
  const [provider, setProvider] = useState(null)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState(null)

  useEffect(() => {
    fetchData()
  }, [])

  const fetchData = async () => {
    setLoading(true)
    setError(null)
    try {
      const res = await fetch('/api/providers/anthropic', {
        credentials: 'include',
        headers: getAuthHeaders()
      })

      if (res.status === 401) {
        onLogout()
        return
      }

      const data = await res.json()
      if (data.error) {
        setError(data.error)
      } else {
        setProvider(data)
      }
    } catch (err) {
      setError(err.message)
    } finally {
      setLoading(false)
    }
  }

  if (loading) {
    return (
      <div className="bg-white rounded-lg p-8 shadow-sm border border-gray-200 text-center">
        <p className="text-gray-500">Loading Anthropic status...</p>
      </div>
    )
  }

  return (
    <div className="space-y-6">
      {/* Provider Status Card */}
      <div className="bg-white rounded-lg shadow-sm border border-gray-200 p-6">
        <div className="flex items-center justify-between mb-4">
          <div className="flex items-center gap-3">
            <div className="w-10 h-10 rounded-lg bg-orange-50 flex items-center justify-center">
              <svg viewBox="0 0 24 24" className="w-6 h-6" fill="#DA7756">
                <path d="M4.709 15.955l4.72-2.647.08-.08 2.726-1.529 6.676-3.614-4.159 7.358c-.16.319-.48.558-.878.638l-5.605 1.056c-.399.08-.798-.08-1.037-.399l-2.046-2.486-.08.08c-.16-.16-.319-.319-.398-.558l.001.181zm6.02-5.943l7.235-4.092c.239-.16.558-.16.877 0l3.016 1.688c.319.16.478.479.399.797l-.4 1.449-7.235 3.933c-.24.16-.559.16-.878 0L10.73 12.1a.845.845 0 01-.399-.798c.079-.479.239-.957.398-1.289zm-6.179 3.933c-.16-.32-.239-.638-.16-.957l.878-5.206c.08-.398.32-.717.638-.877l4.8-2.727c.32-.16.638-.08.878.08l2.886 2.328-5.045 2.886-4.875 4.473z"/>
              </svg>
            </div>
            <div>
              <h2 className="text-lg font-semibold text-gray-900">Anthropic</h2>
              <p className="text-sm text-gray-500">Claude 3.5, Claude 3</p>
            </div>
          </div>
          <StatusBadge status={provider?.status || (error ? 'error' : 'unknown')} />
        </div>

        {error ? (
          <div className="bg-yellow-50 border border-yellow-200 rounded-lg p-4">
            <h3 className="font-semibold text-yellow-800">Anthropic Not Configured</h3>
            <p className="text-sm text-yellow-700 mt-1">{error}</p>
            <p className="text-sm text-yellow-600 mt-2">
              Set ANTHROPIC_API_KEY environment variable to enable this provider.
            </p>
          </div>
        ) : (
          <div className="space-y-4">
            <div className="grid grid-cols-3 gap-4">
              <StatCard label="Default Model" value={provider?.default_model || 'claude-3-5-sonnet'} />
              <StatCard label="Requests Today" value={provider?.requests_today || '0'} />
              <StatCard label="Tokens Today" value={formatNumber(provider?.tokens_today || 0)} />
            </div>

            {provider?.models && (
              <div>
                <h3 className="text-sm font-medium text-gray-700 mb-2">Available Models</h3>
                <div className="flex flex-wrap gap-2">
                  {provider.models.map(model => (
                    <span key={model} className="px-2 py-1 bg-gray-100 text-gray-700 text-xs rounded">
                      {model}
                    </span>
                  ))}
                </div>
              </div>
            )}
          </div>
        )}
      </div>

      {/* External Link */}
      <div className="text-center">
        <a
          href="https://console.anthropic.com/settings/organization"
          target="_blank"
          rel="noopener noreferrer"
          className="text-sm text-gray-500 hover:text-orange-600 inline-flex items-center gap-1"
        >
          View Anthropic Console
          <svg className="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
            <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M10 6H6a2 2 0 00-2 2v10a2 2 0 002 2h10a2 2 0 002-2v-4M14 4h6m0 0v6m0-6L10 14" />
          </svg>
        </a>
      </div>
    </div>
  )
}

function StatusBadge({ status }) {
  const styles = {
    active: 'bg-green-100 text-green-700',
    configured: 'bg-green-100 text-green-700',
    error: 'bg-red-100 text-red-700',
    unknown: 'bg-gray-100 text-gray-600'
  }

  return (
    <span className={`text-xs px-2 py-1 rounded-full font-medium ${styles[status] || styles.unknown}`}>
      {status === 'active' || status === 'configured' ? 'Active' : status}
    </span>
  )
}

function StatCard({ label, value }) {
  return (
    <div className="bg-gray-50 rounded-lg p-3">
      <p className="text-xs text-gray-500">{label}</p>
      <p className="text-lg font-semibold text-gray-900">{value}</p>
    </div>
  )
}

function formatNumber(value) {
  if (value >= 1000000) {
    return (value / 1000000).toFixed(1) + 'M'
  } else if (value >= 1000) {
    return (value / 1000).toFixed(1) + 'K'
  }
  return value.toLocaleString()
}

export default Anthropic
