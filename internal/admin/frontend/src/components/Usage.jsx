import { useState, useEffect } from 'react'
import { AreaChart, Area, XAxis, YAxis, CartesianGrid, Tooltip, ResponsiveContainer, Legend } from 'recharts'

function Usage({ getAuthHeaders, onLogout }) {
  const [usage, setUsage] = useState(null)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState(null)
  const [timeRange, setTimeRange] = useState('7d')

  useEffect(() => {
    fetchUsage()
  }, [timeRange])

  const fetchUsage = async () => {
    setLoading(true)
    setError(null)
    try {
      const res = await fetch(`/api/usage?range=${timeRange}`, {
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
        setUsage(data)
      }
    } catch (err) {
      setError(err.message)
    } finally {
      setLoading(false)
    }
  }

  if (loading && !usage) {
    return (
      <div className="bg-white rounded-lg p-8 shadow-sm border border-gray-200 text-center">
        <p className="text-gray-500">Loading usage data...</p>
      </div>
    )
  }

  return (
    <div className="space-y-6">
      {/* Time Range Selector */}
      <div className="flex items-center justify-between">
        <div className="flex gap-2">
          {['7d', '30d', 'mtd'].map(range => (
            <button
              key={range}
              onClick={() => setTimeRange(range)}
              className={`px-3 py-1.5 rounded-lg text-sm font-medium transition-colors ${
                timeRange === range
                  ? 'bg-blue-100 text-blue-700'
                  : 'bg-gray-100 text-gray-600 hover:bg-gray-200'
              }`}
            >
              {range === '7d' ? '7 Days' : range === '30d' ? '30 Days' : 'Month to Date'}
            </button>
          ))}
        </div>
        <button
          onClick={fetchUsage}
          className="text-sm text-gray-500 hover:text-gray-700"
        >
          Refresh
        </button>
      </div>

      {error ? (
        <div className="bg-yellow-50 border border-yellow-200 rounded-lg p-4">
          <p className="text-yellow-700">{error}</p>
        </div>
      ) : (
        <>
          {/* Summary Stats */}
          <div className="grid grid-cols-4 gap-4">
            <StatCard
              label="Total Requests"
              value={formatNumber(usage?.total_requests || 0)}
              color="blue"
            />
            <StatCard
              label="Total Tokens"
              value={formatNumber(usage?.total_tokens || 0)}
              color="green"
            />
            <StatCard
              label="Input Tokens"
              value={formatNumber(usage?.input_tokens || 0)}
              color="purple"
            />
            <StatCard
              label="Output Tokens"
              value={formatNumber(usage?.output_tokens || 0)}
              color="orange"
            />
          </div>

          {/* Token Usage Chart */}
          <div className="bg-white rounded-lg shadow-sm border border-gray-200 p-6">
            <h3 className="text-lg font-semibold text-gray-900 mb-4">Token Usage Over Time</h3>
            {usage?.daily_tokens && usage.daily_tokens.length > 0 ? (
              <div className="h-64">
                <ResponsiveContainer width="100%" height="100%">
                  <AreaChart data={usage.daily_tokens}>
                    <defs>
                      <linearGradient id="inputGradient" x1="0" y1="0" x2="0" y2="1">
                        <stop offset="5%" stopColor="#8B5CF6" stopOpacity={0.3}/>
                        <stop offset="95%" stopColor="#8B5CF6" stopOpacity={0}/>
                      </linearGradient>
                      <linearGradient id="outputGradient" x1="0" y1="0" x2="0" y2="1">
                        <stop offset="5%" stopColor="#F97316" stopOpacity={0.3}/>
                        <stop offset="95%" stopColor="#F97316" stopOpacity={0}/>
                      </linearGradient>
                    </defs>
                    <CartesianGrid strokeDasharray="3 3" stroke="#E5E7EB" />
                    <XAxis
                      dataKey="date"
                      tick={{ fontSize: 12, fill: '#6B7280' }}
                      tickFormatter={(value) => {
                        const date = new Date(value)
                        return `${date.getMonth() + 1}/${date.getDate()}`
                      }}
                    />
                    <YAxis
                      tick={{ fontSize: 12, fill: '#6B7280' }}
                      tickFormatter={(value) => formatNumber(value)}
                    />
                    <Tooltip
                      formatter={(value) => [formatNumber(value), '']}
                      labelFormatter={(label) => new Date(label).toLocaleDateString()}
                    />
                    <Legend />
                    <Area
                      type="monotone"
                      dataKey="input"
                      name="Input Tokens"
                      stroke="#8B5CF6"
                      fill="url(#inputGradient)"
                    />
                    <Area
                      type="monotone"
                      dataKey="output"
                      name="Output Tokens"
                      stroke="#F97316"
                      fill="url(#outputGradient)"
                    />
                  </AreaChart>
                </ResponsiveContainer>
              </div>
            ) : (
              <div className="h-64 flex items-center justify-center text-gray-500">
                No usage data available for this period
              </div>
            )}
          </div>

          {/* Usage by Provider */}
          {usage?.by_provider && Object.keys(usage.by_provider).length > 0 && (
            <div className="bg-white rounded-lg shadow-sm border border-gray-200 p-6">
              <h3 className="text-lg font-semibold text-gray-900 mb-4">Usage by Provider</h3>
              <div className="space-y-3">
                {Object.entries(usage.by_provider).map(([provider, data]) => (
                  <div key={provider} className="flex items-center justify-between p-3 bg-gray-50 rounded-lg">
                    <div className="flex items-center gap-3">
                      <ProviderIcon provider={provider} />
                      <span className="font-medium text-gray-900 capitalize">{provider}</span>
                    </div>
                    <div className="text-right">
                      <p className="font-medium text-gray-900">{formatNumber(data.tokens || 0)} tokens</p>
                      <p className="text-sm text-gray-500">{formatNumber(data.requests || 0)} requests</p>
                    </div>
                  </div>
                ))}
              </div>
            </div>
          )}

          {/* Usage by Tenant */}
          {usage?.by_tenant && Object.keys(usage.by_tenant).length > 0 && (
            <div className="bg-white rounded-lg shadow-sm border border-gray-200 p-6">
              <h3 className="text-lg font-semibold text-gray-900 mb-4">Usage by Tenant</h3>
              <div className="space-y-3">
                {Object.entries(usage.by_tenant).map(([tenant, data]) => (
                  <div key={tenant} className="flex items-center justify-between p-3 bg-gray-50 rounded-lg">
                    <span className="font-medium text-gray-900">{tenant}</span>
                    <div className="text-right">
                      <p className="font-medium text-gray-900">{formatNumber(data.tokens || 0)} tokens</p>
                      <p className="text-sm text-gray-500">{formatNumber(data.requests || 0)} requests</p>
                    </div>
                  </div>
                ))}
              </div>
            </div>
          )}
        </>
      )}
    </div>
  )
}

function StatCard({ label, value, color }) {
  const colors = {
    blue: 'bg-blue-50 text-blue-700',
    green: 'bg-green-50 text-green-700',
    purple: 'bg-purple-50 text-purple-700',
    orange: 'bg-orange-50 text-orange-700'
  }

  return (
    <div className={`rounded-lg p-4 ${colors[color]}`}>
      <p className="text-xs opacity-75">{label}</p>
      <p className="text-xl font-semibold mt-1">{value}</p>
    </div>
  )
}

function ProviderIcon({ provider }) {
  const icons = {
    openai: (
      <svg viewBox="0 0 24 24" className="w-5 h-5" fill="#10A37F">
        <path d="M22.282 9.821a5.985 5.985 0 0 0-.516-4.91 6.046 6.046 0 0 0-6.51-2.9A6.065 6.065 0 0 0 4.981 4.18a5.985 5.985 0 0 0-3.998 2.9 6.046 6.046 0 0 0 .743 7.097 5.98 5.98 0 0 0 .51 4.911 6.051 6.051 0 0 0 6.515 2.9A5.985 5.985 0 0 0 13.26 24a6.056 6.056 0 0 0 5.772-4.206 5.99 5.99 0 0 0 3.997-2.9 6.056 6.056 0 0 0-.747-7.073z"/>
      </svg>
    ),
    anthropic: (
      <svg viewBox="0 0 24 24" className="w-5 h-5" fill="#DA7756">
        <path d="M4.709 15.955l4.72-2.647.08-.08 2.726-1.529 6.676-3.614-4.159 7.358c-.16.319-.48.558-.878.638l-5.605 1.056c-.399.08-.798-.08-1.037-.399l-2.046-2.486z"/>
      </svg>
    ),
    gemini: (
      <svg viewBox="0 0 24 24" className="w-5 h-5" fill="#4285F4">
        <path d="M12 2C12 2 13.5 9 14.5 10.5C16 11.5 22 12 22 12C22 12 16 12.5 14.5 13.5C13.5 15 12 22 12 22C12 22 10.5 15 9.5 13.5C8 12.5 2 12 2 12C2 12 8 11.5 9.5 10.5C10.5 9 12 2 12 2Z"/>
      </svg>
    )
  }

  return icons[provider] || <span className="w-5 h-5 bg-gray-300 rounded-full" />
}

function formatNumber(value) {
  if (value >= 1000000) {
    return (value / 1000000).toFixed(1) + 'M'
  } else if (value >= 1000) {
    return (value / 1000).toFixed(1) + 'K'
  }
  return value.toLocaleString()
}

export default Usage
