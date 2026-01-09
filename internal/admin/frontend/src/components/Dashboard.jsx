function Dashboard({ info, stats, loading }) {
  if (loading && !info) {
    return (
      <div className="bg-white rounded-lg p-8 shadow-sm border border-gray-200 text-center">
        <p className="text-gray-500">Loading dashboard...</p>
      </div>
    )
  }

  return (
    <div className="space-y-6">
      {/* Server Info */}
      <div className="bg-white rounded-lg shadow-sm border border-gray-200 p-6">
        <h2 className="text-lg font-semibold text-gray-900 mb-4">Server Status</h2>
        <div className="grid grid-cols-4 gap-4">
          <StatCard
            label="Status"
            value={info?.status || 'Unknown'}
            color={info?.status === 'healthy' ? 'green' : 'yellow'}
          />
          <StatCard
            label="Version"
            value={info?.version || '-'}
            color="blue"
          />
          <StatCard
            label="Uptime"
            value={formatUptime(info?.uptime_seconds)}
            color="purple"
          />
          <StatCard
            label="Git Commit"
            value={info?.git_commit?.slice(0, 7) || '-'}
            color="gray"
          />
        </div>
      </div>

      {/* Quick Stats */}
      <div className="bg-white rounded-lg shadow-sm border border-gray-200 p-6">
        <h2 className="text-lg font-semibold text-gray-900 mb-4">Quick Stats</h2>
        <div className="grid grid-cols-4 gap-4">
          <StatCard
            label="API Keys"
            value={stats?.api_key_count || 0}
            color="blue"
          />
          <StatCard
            label="Active Tenants"
            value={stats?.tenant_count || 0}
            color="green"
          />
          <StatCard
            label="Requests Today"
            value={formatNumber(stats?.requests_today || 0)}
            color="orange"
          />
          <StatCard
            label="Tokens Today"
            value={formatNumber(stats?.tokens_today || 0)}
            color="purple"
          />
        </div>
      </div>

      {/* Provider Status */}
      <div className="bg-white rounded-lg shadow-sm border border-gray-200 p-6">
        <h2 className="text-lg font-semibold text-gray-900 mb-4">Provider Status</h2>
        <div className="grid grid-cols-3 gap-4">
          <ProviderCard
            name="OpenAI"
            status={stats?.providers?.openai || 'unknown'}
            icon={<OpenAIIcon />}
          />
          <ProviderCard
            name="Anthropic"
            status={stats?.providers?.anthropic || 'unknown'}
            icon={<AnthropicIcon />}
          />
          <ProviderCard
            name="Gemini"
            status={stats?.providers?.gemini || 'unknown'}
            icon={<GeminiIcon />}
          />
        </div>
      </div>
    </div>
  )
}

function StatCard({ label, value, color }) {
  const colors = {
    green: 'bg-green-50 text-green-700',
    blue: 'bg-blue-50 text-blue-700',
    purple: 'bg-purple-50 text-purple-700',
    orange: 'bg-orange-50 text-orange-700',
    yellow: 'bg-yellow-50 text-yellow-700',
    gray: 'bg-gray-50 text-gray-700'
  }

  return (
    <div className={`rounded-lg p-4 ${colors[color] || colors.gray}`}>
      <p className="text-xs opacity-75">{label}</p>
      <p className="text-xl font-semibold mt-1">{value}</p>
    </div>
  )
}

function ProviderCard({ name, status, icon }) {
  const isActive = status === 'active' || status === 'configured'

  return (
    <div className={`rounded-lg p-4 border ${isActive ? 'border-green-200 bg-green-50' : 'border-gray-200 bg-gray-50'}`}>
      <div className="flex items-center gap-3">
        <div className="w-8 h-8">{icon}</div>
        <div>
          <p className="font-medium text-gray-900">{name}</p>
          <p className={`text-xs ${isActive ? 'text-green-600' : 'text-gray-500'}`}>
            {isActive ? 'Active' : 'Not configured'}
          </p>
        </div>
      </div>
    </div>
  )
}

function formatUptime(seconds) {
  if (!seconds) return '-'
  const days = Math.floor(seconds / 86400)
  const hours = Math.floor((seconds % 86400) / 3600)
  const mins = Math.floor((seconds % 3600) / 60)

  if (days > 0) return `${days}d ${hours}h`
  if (hours > 0) return `${hours}h ${mins}m`
  return `${mins}m`
}

function formatNumber(value) {
  if (value >= 1000000) {
    return (value / 1000000).toFixed(1) + 'M'
  } else if (value >= 1000) {
    return (value / 1000).toFixed(1) + 'K'
  }
  return value.toLocaleString()
}

// Provider Icons
const OpenAIIcon = () => (
  <svg viewBox="0 0 24 24" className="w-full h-full" fill="#10A37F">
    <path d="M22.282 9.821a5.985 5.985 0 0 0-.516-4.91 6.046 6.046 0 0 0-6.51-2.9A6.065 6.065 0 0 0 4.981 4.18a5.985 5.985 0 0 0-3.998 2.9 6.046 6.046 0 0 0 .743 7.097 5.98 5.98 0 0 0 .51 4.911 6.051 6.051 0 0 0 6.515 2.9A5.985 5.985 0 0 0 13.26 24a6.056 6.056 0 0 0 5.772-4.206 5.99 5.99 0 0 0 3.997-2.9 6.056 6.056 0 0 0-.747-7.073z"/>
  </svg>
)

const AnthropicIcon = () => (
  <svg viewBox="0 0 24 24" className="w-full h-full" fill="#DA7756">
    <path d="M4.709 15.955l4.72-2.647.08-.08 2.726-1.529 6.676-3.614-4.159 7.358c-.16.319-.48.558-.878.638l-5.605 1.056c-.399.08-.798-.08-1.037-.399l-2.046-2.486-.08.08c-.16-.16-.319-.319-.398-.558l.001.181z"/>
  </svg>
)

const GeminiIcon = () => (
  <svg viewBox="0 0 24 24" className="w-full h-full">
    <defs>
      <linearGradient id="dashGemini" x1="0%" y1="0%" x2="100%" y2="100%">
        <stop offset="0%" stopColor="#4285F4"/>
        <stop offset="50%" stopColor="#9B72CB"/>
        <stop offset="100%" stopColor="#D96570"/>
      </linearGradient>
    </defs>
    <path fill="url(#dashGemini)" d="M12 2C12 2 13.5 9 14.5 10.5C16 11.5 22 12 22 12C22 12 16 12.5 14.5 13.5C13.5 15 12 22 12 22C12 22 10.5 15 9.5 13.5C8 12.5 2 12 2 12C2 12 8 11.5 9.5 10.5C10.5 9 12 2 12 2Z"/>
  </svg>
)

export default Dashboard
