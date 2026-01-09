import { useState, useEffect } from 'react'

function Gemini({ getAuthHeaders, onLogout }) {
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
      const res = await fetch('/api/providers/gemini', {
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
        <p className="text-gray-500">Loading Gemini status...</p>
      </div>
    )
  }

  return (
    <div className="space-y-6">
      {/* Provider Status Card */}
      <div className="bg-white rounded-lg shadow-sm border border-gray-200 p-6">
        <div className="flex items-center justify-between mb-4">
          <div className="flex items-center gap-3">
            <div className="w-10 h-10 rounded-lg bg-blue-50 flex items-center justify-center">
              <svg viewBox="0 0 24 24" className="w-6 h-6">
                <defs>
                  <linearGradient id="geminiGradient" x1="0%" y1="0%" x2="100%" y2="100%">
                    <stop offset="0%" stopColor="#4285F4"/>
                    <stop offset="50%" stopColor="#9B72CB"/>
                    <stop offset="100%" stopColor="#D96570"/>
                  </linearGradient>
                </defs>
                <path fill="url(#geminiGradient)" d="M12 2C12 2 13.5 9 14.5 10.5C16 11.5 22 12 22 12C22 12 16 12.5 14.5 13.5C13.5 15 12 22 12 22C12 22 10.5 15 9.5 13.5C8 12.5 2 12 2 12C2 12 8 11.5 9.5 10.5C10.5 9 12 2 12 2Z"/>
              </svg>
            </div>
            <div>
              <h2 className="text-lg font-semibold text-gray-900">Google Gemini</h2>
              <p className="text-sm text-gray-500">Gemini Pro, Gemini Flash</p>
            </div>
          </div>
          <StatusBadge status={provider?.status || (error ? 'error' : 'unknown')} />
        </div>

        {error ? (
          <div className="bg-yellow-50 border border-yellow-200 rounded-lg p-4">
            <h3 className="font-semibold text-yellow-800">Gemini Not Configured</h3>
            <p className="text-sm text-yellow-700 mt-1">{error}</p>
            <p className="text-sm text-yellow-600 mt-2">
              Set GEMINI_API_KEY environment variable to enable this provider.
            </p>
          </div>
        ) : (
          <div className="space-y-4">
            <div className="grid grid-cols-3 gap-4">
              <StatCard label="Default Model" value={provider?.default_model || 'gemini-pro'} />
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
          href="https://aistudio.google.com/"
          target="_blank"
          rel="noopener noreferrer"
          className="text-sm text-gray-500 hover:text-blue-600 inline-flex items-center gap-1"
        >
          View Google AI Studio
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

export default Gemini
