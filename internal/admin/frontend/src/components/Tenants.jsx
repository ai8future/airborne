import { useState, useEffect } from 'react'

function Tenants({ getAuthHeaders, onLogout }) {
  const [tenants, setTenants] = useState([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState(null)
  const [selectedTenant, setSelectedTenant] = useState(null)

  useEffect(() => {
    fetchTenants()
  }, [])

  const fetchTenants = async () => {
    setLoading(true)
    setError(null)
    try {
      const res = await fetch('/api/tenants', {
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
        setTenants(data.tenants || [])
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
        <p className="text-gray-500">Loading tenants...</p>
      </div>
    )
  }

  return (
    <div className="space-y-6">
      {/* Header */}
      <div className="flex items-center justify-between">
        <h2 className="text-lg font-semibold text-gray-900">Tenants</h2>
        <p className="text-sm text-gray-500">
          Tenant configurations are managed via YAML files
        </p>
      </div>

      {error ? (
        <div className="bg-yellow-50 border border-yellow-200 rounded-lg p-4">
          <p className="text-yellow-700">{error}</p>
        </div>
      ) : tenants.length === 0 ? (
        <div className="bg-white rounded-lg shadow-sm border border-gray-200 p-8 text-center">
          <p className="text-gray-500">No tenants configured.</p>
          <p className="text-sm text-gray-400 mt-2">
            Add tenant YAML files to configs/tenants/ to configure multi-tenant support.
          </p>
        </div>
      ) : (
        <div className="grid gap-4">
          {tenants.map(tenant => (
            <div
              key={tenant.code}
              className="bg-white rounded-lg shadow-sm border border-gray-200 p-4 hover:border-blue-300 transition-colors cursor-pointer"
              onClick={() => setSelectedTenant(selectedTenant?.code === tenant.code ? null : tenant)}
            >
              <div className="flex items-center justify-between">
                <div className="flex items-center gap-3">
                  <div className="w-10 h-10 rounded-lg bg-blue-50 flex items-center justify-center text-lg">
                    {tenant.name?.[0]?.toUpperCase() || '?'}
                  </div>
                  <div>
                    <h3 className="font-medium text-gray-900">{tenant.name}</h3>
                    <p className="text-sm text-gray-500">Code: {tenant.code}</p>
                  </div>
                </div>
                <div className="flex items-center gap-4">
                  <div className="text-right">
                    <p className="text-sm text-gray-600">Default Provider</p>
                    <p className="font-medium text-gray-900">{tenant.default_provider || 'Not set'}</p>
                  </div>
                  <svg
                    className={`w-5 h-5 text-gray-400 transition-transform ${selectedTenant?.code === tenant.code ? 'rotate-180' : ''}`}
                    fill="none"
                    stroke="currentColor"
                    viewBox="0 0 24 24"
                  >
                    <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M19 9l-7 7-7-7" />
                  </svg>
                </div>
              </div>

              {/* Expanded Details */}
              {selectedTenant?.code === tenant.code && (
                <div className="mt-4 pt-4 border-t border-gray-200">
                  <div className="grid grid-cols-2 gap-4">
                    <div>
                      <h4 className="text-sm font-medium text-gray-700 mb-2">Provider Settings</h4>
                      <div className="space-y-2">
                        {tenant.providers ? (
                          Object.entries(tenant.providers).map(([provider, config]) => (
                            <div key={provider} className="flex justify-between text-sm">
                              <span className="text-gray-600">{provider}</span>
                              <span className="text-gray-900">{config.model || 'default'}</span>
                            </div>
                          ))
                        ) : (
                          <p className="text-sm text-gray-500">Using default provider settings</p>
                        )}
                      </div>
                    </div>
                    <div>
                      <h4 className="text-sm font-medium text-gray-700 mb-2">Rate Limits</h4>
                      <div className="space-y-2 text-sm">
                        <div className="flex justify-between">
                          <span className="text-gray-600">RPM</span>
                          <span className="text-gray-900">{tenant.rate_limits?.rpm || 'default'}</span>
                        </div>
                        <div className="flex justify-between">
                          <span className="text-gray-600">RPD</span>
                          <span className="text-gray-900">{tenant.rate_limits?.rpd || 'default'}</span>
                        </div>
                        <div className="flex justify-between">
                          <span className="text-gray-600">TPM</span>
                          <span className="text-gray-900">{tenant.rate_limits?.tpm || 'default'}</span>
                        </div>
                      </div>
                    </div>
                  </div>

                  {tenant.allowed_models && (
                    <div className="mt-4">
                      <h4 className="text-sm font-medium text-gray-700 mb-2">Allowed Models</h4>
                      <div className="flex flex-wrap gap-2">
                        {tenant.allowed_models.map(model => (
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
          ))}
        </div>
      )}
    </div>
  )
}

export default Tenants
