import { useState, useEffect } from 'react'
import { useToastActions } from './ToastProvider'

function ApiKeys({ getAuthHeaders, onLogout }) {
  const [keys, setKeys] = useState([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState(null)
  const [showCreateForm, setShowCreateForm] = useState(false)
  const [newKey, setNewKey] = useState(null)
  const toast = useToastActions()

  // Create form state
  const [formData, setFormData] = useState({
    client_name: '',
    permissions: ['chat'],
    rpm: 60,
    rpd: 1000,
    tpm: 100000
  })
  const [creating, setCreating] = useState(false)

  useEffect(() => {
    fetchKeys()
  }, [])

  const fetchKeys = async () => {
    setLoading(true)
    setError(null)
    try {
      const res = await fetch('/api/keys', {
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
        setKeys(data.keys || [])
      }
    } catch (err) {
      setError(err.message)
    } finally {
      setLoading(false)
    }
  }

  const handleCreate = async (e) => {
    e.preventDefault()
    setCreating(true)
    try {
      const res = await fetch('/api/keys', {
        method: 'POST',
        credentials: 'include',
        headers: {
          'Content-Type': 'application/json',
          ...getAuthHeaders()
        },
        body: JSON.stringify(formData)
      })

      if (res.status === 401) {
        onLogout()
        return
      }

      const data = await res.json()
      if (data.error) {
        toast.error(data.error)
      } else {
        setNewKey(data.key) // Show the full key once
        setShowCreateForm(false)
        setFormData({
          client_name: '',
          permissions: ['chat'],
          rpm: 60,
          rpd: 1000,
          tpm: 100000
        })
        fetchKeys()
        toast.success('API key created')
      }
    } catch (err) {
      toast.error(err.message)
    } finally {
      setCreating(false)
    }
  }

  const handleRevoke = async (keyId, clientName) => {
    if (!confirm(`Revoke API key for "${clientName}"? This cannot be undone.`)) return

    try {
      const res = await fetch(`/api/keys/${keyId}`, {
        method: 'DELETE',
        credentials: 'include',
        headers: getAuthHeaders()
      })

      if (res.status === 401) {
        onLogout()
        return
      }

      const data = await res.json()
      if (data.error) {
        toast.error(data.error)
      } else {
        toast.success('API key revoked')
        fetchKeys()
      }
    } catch (err) {
      toast.error(err.message)
    }
  }

  const copyToClipboard = (text) => {
    navigator.clipboard.writeText(text)
    toast.success('Copied to clipboard')
  }

  const togglePermission = (perm) => {
    setFormData(prev => ({
      ...prev,
      permissions: prev.permissions.includes(perm)
        ? prev.permissions.filter(p => p !== perm)
        : [...prev.permissions, perm]
    }))
  }

  if (loading) {
    return (
      <div className="bg-white rounded-lg p-8 shadow-sm border border-gray-200 text-center">
        <p className="text-gray-500">Loading API keys...</p>
      </div>
    )
  }

  return (
    <div className="space-y-6">
      {/* New Key Display */}
      {newKey && (
        <div className="bg-green-50 border border-green-200 rounded-lg p-4">
          <div className="flex items-start justify-between">
            <div>
              <h3 className="font-semibold text-green-800">API Key Created</h3>
              <p className="text-sm text-green-700 mt-1">
                Copy this key now. You won't be able to see it again.
              </p>
              <code className="block mt-2 p-2 bg-white rounded border border-green-300 text-sm font-mono break-all">
                {newKey}
              </code>
            </div>
            <div className="flex gap-2">
              <button
                onClick={() => copyToClipboard(newKey)}
                className="px-3 py-1 text-sm bg-green-600 text-white rounded hover:bg-green-700"
              >
                Copy
              </button>
              <button
                onClick={() => setNewKey(null)}
                className="px-3 py-1 text-sm bg-gray-200 text-gray-700 rounded hover:bg-gray-300"
              >
                Dismiss
              </button>
            </div>
          </div>
        </div>
      )}

      {/* Header */}
      <div className="flex items-center justify-between">
        <h2 className="text-lg font-semibold text-gray-900">API Keys</h2>
        <button
          onClick={() => setShowCreateForm(true)}
          className="px-4 py-2 bg-blue-600 text-white rounded-lg hover:bg-blue-700 text-sm"
        >
          Create New Key
        </button>
      </div>

      {/* Create Form Modal */}
      {showCreateForm && (
        <div className="fixed inset-0 bg-black bg-opacity-50 flex items-center justify-center z-50">
          <div className="bg-white rounded-lg shadow-xl p-6 w-full max-w-md">
            <h3 className="text-lg font-semibold mb-4">Create API Key</h3>
            <form onSubmit={handleCreate} className="space-y-4">
              <div>
                <label className="block text-sm font-medium text-gray-700 mb-1">
                  Client Name
                </label>
                <input
                  type="text"
                  value={formData.client_name}
                  onChange={(e) => setFormData(prev => ({ ...prev, client_name: e.target.value }))}
                  className="w-full px-3 py-2 border border-gray-300 rounded-lg focus:ring-2 focus:ring-blue-500"
                  placeholder="My Application"
                  required
                />
              </div>

              <div>
                <label className="block text-sm font-medium text-gray-700 mb-2">
                  Permissions
                </label>
                <div className="flex flex-wrap gap-2">
                  {['chat', 'chat:stream', 'files', 'admin'].map(perm => (
                    <button
                      key={perm}
                      type="button"
                      onClick={() => togglePermission(perm)}
                      className={`px-3 py-1 rounded text-sm ${
                        formData.permissions.includes(perm)
                          ? 'bg-blue-100 text-blue-700 border border-blue-300'
                          : 'bg-gray-100 text-gray-600 border border-gray-200'
                      }`}
                    >
                      {perm}
                    </button>
                  ))}
                </div>
              </div>

              <div className="grid grid-cols-3 gap-3">
                <div>
                  <label className="block text-xs font-medium text-gray-700 mb-1">RPM</label>
                  <input
                    type="number"
                    value={formData.rpm}
                    onChange={(e) => setFormData(prev => ({ ...prev, rpm: parseInt(e.target.value) }))}
                    className="w-full px-2 py-1 border border-gray-300 rounded text-sm"
                  />
                </div>
                <div>
                  <label className="block text-xs font-medium text-gray-700 mb-1">RPD</label>
                  <input
                    type="number"
                    value={formData.rpd}
                    onChange={(e) => setFormData(prev => ({ ...prev, rpd: parseInt(e.target.value) }))}
                    className="w-full px-2 py-1 border border-gray-300 rounded text-sm"
                  />
                </div>
                <div>
                  <label className="block text-xs font-medium text-gray-700 mb-1">TPM</label>
                  <input
                    type="number"
                    value={formData.tpm}
                    onChange={(e) => setFormData(prev => ({ ...prev, tpm: parseInt(e.target.value) }))}
                    className="w-full px-2 py-1 border border-gray-300 rounded text-sm"
                  />
                </div>
              </div>

              <div className="flex justify-end gap-2 pt-4">
                <button
                  type="button"
                  onClick={() => setShowCreateForm(false)}
                  className="px-4 py-2 text-gray-700 hover:bg-gray-100 rounded-lg"
                >
                  Cancel
                </button>
                <button
                  type="submit"
                  disabled={creating}
                  className="px-4 py-2 bg-blue-600 text-white rounded-lg hover:bg-blue-700 disabled:opacity-50"
                >
                  {creating ? 'Creating...' : 'Create Key'}
                </button>
              </div>
            </form>
          </div>
        </div>
      )}

      {/* Keys Table */}
      <div className="bg-white rounded-lg shadow-sm border border-gray-200 overflow-hidden">
        {error ? (
          <div className="p-4 text-center text-red-600">{error}</div>
        ) : keys.length === 0 ? (
          <div className="p-8 text-center text-gray-500">
            No API keys yet. Create one to get started.
          </div>
        ) : (
          <table className="w-full">
            <thead className="bg-gray-50 border-b border-gray-200">
              <tr>
                <th className="px-4 py-3 text-left text-xs font-medium text-gray-500 uppercase">Client</th>
                <th className="px-4 py-3 text-left text-xs font-medium text-gray-500 uppercase">Key ID</th>
                <th className="px-4 py-3 text-left text-xs font-medium text-gray-500 uppercase">Permissions</th>
                <th className="px-4 py-3 text-left text-xs font-medium text-gray-500 uppercase">Rate Limits</th>
                <th className="px-4 py-3 text-left text-xs font-medium text-gray-500 uppercase">Last Used</th>
                <th className="px-4 py-3 text-right text-xs font-medium text-gray-500 uppercase">Actions</th>
              </tr>
            </thead>
            <tbody className="divide-y divide-gray-200">
              {keys.map(key => (
                <tr key={key.id} className="hover:bg-gray-50">
                  <td className="px-4 py-3">
                    <span className="font-medium text-gray-900">{key.client_name}</span>
                  </td>
                  <td className="px-4 py-3">
                    <code className="text-sm text-gray-600">{key.key_id}</code>
                  </td>
                  <td className="px-4 py-3">
                    <div className="flex flex-wrap gap-1">
                      {key.permissions?.map(perm => (
                        <span key={perm} className="px-2 py-0.5 bg-gray-100 text-gray-600 text-xs rounded">
                          {perm}
                        </span>
                      ))}
                    </div>
                  </td>
                  <td className="px-4 py-3 text-sm text-gray-600">
                    {key.rate_limits?.rpm || '-'} RPM
                  </td>
                  <td className="px-4 py-3 text-sm text-gray-500">
                    {key.last_used ? new Date(key.last_used).toLocaleDateString() : 'Never'}
                  </td>
                  <td className="px-4 py-3 text-right">
                    <button
                      onClick={() => handleRevoke(key.id, key.client_name)}
                      className="text-red-600 hover:text-red-800 text-sm"
                    >
                      Revoke
                    </button>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        )}
      </div>
    </div>
  )
}

export default ApiKeys
