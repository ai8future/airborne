import { useState } from 'react'

// AI Provider Icons
const OpenAIIcon = () => (
  <svg viewBox="0 0 24 24" className="w-5 h-5" fill="#10A37F">
    <path d="M22.282 9.821a5.985 5.985 0 0 0-.516-4.91 6.046 6.046 0 0 0-6.51-2.9A6.065 6.065 0 0 0 4.981 4.18a5.985 5.985 0 0 0-3.998 2.9 6.046 6.046 0 0 0 .743 7.097 5.98 5.98 0 0 0 .51 4.911 6.051 6.051 0 0 0 6.515 2.9A5.985 5.985 0 0 0 13.26 24a6.056 6.056 0 0 0 5.772-4.206 5.99 5.99 0 0 0 3.997-2.9 6.056 6.056 0 0 0-.747-7.073zM13.26 22.43a4.476 4.476 0 0 1-2.876-1.04l.141-.081 4.779-2.758a.795.795 0 0 0 .392-.681v-6.737l2.02 1.168a.071.071 0 0 1 .038.052v5.583a4.504 4.504 0 0 1-4.494 4.494zM3.6 18.304a4.47 4.47 0 0 1-.535-3.014l.142.085 4.783 2.759a.771.771 0 0 0 .78 0l5.843-3.369v2.332a.08.08 0 0 1-.033.062L9.74 19.95a4.5 4.5 0 0 1-6.14-1.646zM2.34 7.896a4.485 4.485 0 0 1 2.366-1.973V11.6a.766.766 0 0 0 .388.676l5.815 3.355-2.02 1.168a.076.076 0 0 1-.071 0l-4.83-2.786A4.504 4.504 0 0 1 2.34 7.872zm16.597 3.855l-5.833-3.387L15.119 7.2a.076.076 0 0 1 .071 0l4.83 2.791a4.494 4.494 0 0 1-.676 8.105v-5.678a.79.79 0 0 0-.407-.667zm2.01-3.023l-.141-.085-4.774-2.782a.776.776 0 0 0-.785 0L9.409 9.23V6.897a.066.066 0 0 1 .028-.061l4.83-2.787a4.5 4.5 0 0 1 6.68 4.66zm-12.64 4.135l-2.02-1.164a.08.08 0 0 1-.038-.057V6.075a4.5 4.5 0 0 1 7.375-3.453l-.142.08-4.778 2.758a.795.795 0 0 0-.393.681zm1.097-2.365l2.602-1.5 2.607 1.5v2.999l-2.597 1.5-2.607-1.5z"/>
  </svg>
)

const ClaudeIcon = () => (
  <svg viewBox="0 0 24 24" className="w-5 h-5" fill="#DA7756">
    <path d="M4.709 15.955l4.72-2.647.08-.08 2.726-1.529 6.676-3.614-4.159 7.358c-.16.319-.48.558-.878.638l-5.605 1.056c-.399.08-.798-.08-1.037-.399l-2.046-2.486-.08.08c-.16-.16-.319-.319-.398-.558l.001.181zm6.02-5.943l7.235-4.092c.239-.16.558-.16.877 0l3.016 1.688c.319.16.478.479.399.797l-.4 1.449-7.235 3.933c-.24.16-.559.16-.878 0L10.73 12.1a.845.845 0 01-.399-.798c.079-.479.239-.957.398-1.289zm-6.179 3.933c-.16-.32-.239-.638-.16-.957l.878-5.206c.08-.398.32-.717.638-.877l4.8-2.727c.32-.16.638-.08.878.08l2.886 2.328-5.045 2.886-4.875 4.473z"/>
  </svg>
)

const GeminiIcon = () => (
  <svg viewBox="0 0 24 24" className="w-5 h-5">
    <defs>
      <linearGradient id="geminiGradient" x1="0%" y1="0%" x2="100%" y2="100%">
        <stop offset="0%" stopColor="#4285F4"/>
        <stop offset="50%" stopColor="#9B72CB"/>
        <stop offset="100%" stopColor="#D96570"/>
      </linearGradient>
    </defs>
    <path fill="url(#geminiGradient)" d="M12 2C12 2 13.5 9 14.5 10.5C16 11.5 22 12 22 12C22 12 16 12.5 14.5 13.5C13.5 15 12 22 12 22C12 22 10.5 15 9.5 13.5C8 12.5 2 12 2 12C2 12 8 11.5 9.5 10.5C10.5 9 12 2 12 2Z"/>
  </svg>
)

function Sidebar({ activeView, onNavigate, version }) {
  const [expandedGroups, setExpandedGroups] = useState({
    providers: true
  })

  const toggleGroup = (groupId) => {
    setExpandedGroups(prev => ({ ...prev, [groupId]: !prev[groupId] }))
  }

  const navItems = [
    { id: 'dashboard', label: 'Dashboard', icon: '📊' },
    { id: 'api-keys', label: 'API Keys', icon: '🔑' },
    { id: 'tenants', label: 'Tenants', icon: '🏢' },
    { id: 'usage', label: 'Usage', icon: '📈' },
    // AI Providers group
    { id: 'providers', label: 'AI Providers', icon: '🤖', group: 'providers' },
    { id: 'openai', label: 'OpenAI', icon: <OpenAIIcon />, parent: 'providers' },
    { id: 'anthropic', label: 'Anthropic', icon: <ClaudeIcon />, parent: 'providers' },
    { id: 'gemini', label: 'Gemini', icon: <GeminiIcon />, parent: 'providers' },
  ]

  const isChildActive = (parentId) => {
    return navItems
      .filter(item => item.parent === parentId)
      .some(item => item.id === activeView)
  }

  const shouldShowChild = (item) => {
    if (!item.parent) return true
    return expandedGroups[item.parent] || isChildActive(item.parent)
  }

  return (
    <div className="w-64 bg-white border-r border-gray-200 flex flex-col">
      {/* Logo */}
      <div className="p-4 border-b border-gray-200">
        <div className="flex items-center gap-2">
          <span className="text-2xl">🤖</span>
          <div>
            <h1 className="font-bold text-gray-900">AIBox Admin</h1>
            {version && <span className="text-xs text-gray-500">v{version}</span>}
          </div>
        </div>
      </div>

      {/* Navigation */}
      <nav className="flex-1 overflow-y-auto p-2">
        {navItems.filter(shouldShowChild).map((item) => {
          const isGroup = item.group
          const isChild = item.parent
          const isActive = activeView === item.id

          if (isGroup) {
            // Render collapsible group header
            return (
              <button
                key={item.id}
                onClick={() => toggleGroup(item.group)}
                className="w-full flex items-center gap-2 px-3 py-2 mt-4 mb-1 text-xs font-semibold text-gray-500 uppercase tracking-wider hover:text-gray-700"
              >
                <span>{typeof item.icon === 'string' ? item.icon : item.icon}</span>
                <span className="flex-1 text-left">{item.label}</span>
                <svg
                  className={`w-4 h-4 transition-transform ${expandedGroups[item.group] ? 'rotate-180' : ''}`}
                  fill="none"
                  stroke="currentColor"
                  viewBox="0 0 24 24"
                >
                  <path strokeLinecap="round" strokeLinejoin="round" strokeWidth="2" d="M19 9l-7 7-7-7" />
                </svg>
              </button>
            )
          }

          return (
            <button
              key={item.id}
              onClick={() => onNavigate(item.id)}
              className={`
                w-full flex items-center gap-2 px-3 py-2 rounded-lg text-left transition-colors
                ${isChild ? 'ml-4 text-sm' : ''}
                ${isActive
                  ? 'bg-blue-50 text-blue-700'
                  : 'text-gray-700 hover:bg-gray-50'
                }
              `}
            >
              <span className="flex-shrink-0">
                {typeof item.icon === 'string' ? item.icon : item.icon}
              </span>
              <span>{item.label}</span>
            </button>
          )
        })}
      </nav>

      {/* Footer */}
      <div className="p-4 border-t border-gray-200 text-xs text-gray-500">
        <p>Auto-refresh: 30s</p>
        <p className="mt-1">gRPC: localhost:50051</p>
      </div>
    </div>
  )
}

export default Sidebar
