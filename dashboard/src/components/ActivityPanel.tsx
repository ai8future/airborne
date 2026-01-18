"use client";

import { useState, useEffect, useCallback } from "react";

interface ActivityEntry {
  id: string;
  thread_id: string;
  tenant: string;
  user_id: string;
  content: string;
  full_content?: string;
  provider: string;
  model: string;
  input_tokens: number;
  output_tokens: number;
  tokens_used: number;
  cost_usd: number;
  thread_cost_usd: number;
  processing_time_ms: number;
  status: string;
  timestamp: string;
}

export default function ActivityPanel() {
  const [paused, setPaused] = useState(false);
  const [activity, setActivity] = useState<ActivityEntry[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [selectedEntry, setSelectedEntry] = useState<ActivityEntry | null>(null);

  // Fetch activity from backend
  const fetchActivity = useCallback(async () => {
    try {
      const res = await fetch("/api/activity?limit=50");
      const data = await res.json();

      if (data.error) {
        setError(data.error);
      } else {
        setActivity(data.activity || []);
        setError(null);
      }
    } catch (e) {
      setError(`Failed to fetch activity: ${e instanceof Error ? e.message : "Unknown error"}`);
    } finally {
      setLoading(false);
    }
  }, []);

  // Initial fetch and polling
  useEffect(() => {
    fetchActivity();

    // Poll every 3 seconds when not paused
    const interval = setInterval(() => {
      if (!paused) {
        fetchActivity();
      }
    }, 3000);

    return () => clearInterval(interval);
  }, [paused, fetchActivity]);

  // Format token counts
  const formatTokens = (n: number | undefined): string => {
    if (!n) return "-";
    if (n < 1000) return n.toString();
    if (n < 10000) return n.toLocaleString();
    return (n / 1000).toFixed(1) + "K";
  };

  // Get provider badge color
  const getProviderColor = (provider: string): string => {
    const p = provider?.toLowerCase();
    if (p === "gemini") return "bg-cyan-100 text-cyan-700";
    if (p === "anthropic") return "bg-amber-100 text-amber-700";
    return "bg-emerald-100 text-emerald-700";
  };

  // Get model display name (shortened)
  const getModelName = (model: string): string => {
    if (!model) return "";
    return model
      .replace(/^claude-/, "")
      .replace(/^gpt-/, "")
      .replace(/^gemini-/, "")
      .replace(/-20\d{6}$/, "");
  };

  return (
    <div className="space-y-6">
      <div className="bg-white rounded-lg border border-gray-200 shadow-sm">
        <div className="px-4 py-3 border-b border-gray-200 flex items-center justify-between">
          <div className="flex items-center gap-3">
            <h3 className="font-semibold text-gray-800">Live Activity Feed</h3>
            {!paused && (
              <span className="flex items-center gap-1.5 text-xs text-green-600">
                <span className="w-2 h-2 bg-green-500 rounded-full animate-pulse"></span>
                Live
              </span>
            )}
          </div>
          <div className="flex items-center gap-2">
            <button
              onClick={() => setPaused(!paused)}
              className={`px-3 py-1.5 rounded text-sm font-medium transition-colors ${
                paused
                  ? "bg-green-100 text-green-700 hover:bg-green-200"
                  : "bg-gray-100 text-gray-700 hover:bg-gray-200"
              }`}
            >
              {paused ? "Resume" : "Pause"}
            </button>
            <button
              onClick={() => setActivity([])}
              className="px-3 py-1.5 bg-gray-100 text-gray-700 rounded text-sm font-medium hover:bg-gray-200 transition-colors"
            >
              Clear
            </button>
          </div>
        </div>

        {/* Activity Table */}
        <div className="max-h-[calc(100vh-200px)] overflow-y-auto">
          {loading ? (
            <div className="px-4 py-12 text-center text-gray-500">Loading activity...</div>
          ) : error ? (
            <div className="px-4 py-12 text-center text-red-500">{error}</div>
          ) : activity.length === 0 ? (
            <div className="px-4 py-12 text-center text-gray-500">
              No recent activity. Requests will appear here as they are processed.
            </div>
          ) : (
            <table className="w-full text-sm">
              <thead className="sticky top-0 bg-gray-50 border-b border-gray-200 z-10">
                <tr className="text-xs text-gray-500 uppercase tracking-wider">
                  <th className="w-6 px-2 py-3"></th>
                  <th className="w-20 px-2 py-3 text-left align-bottom">Time</th>
                  <th className="w-24 px-2 py-3 text-left align-bottom">Tenant</th>
                  <th className="px-2 py-3 text-left align-bottom">Content</th>
                  <th className="w-14 px-2 py-3 text-right align-bottom">Dur</th>
                  <th className="w-16 px-2 py-3 text-right align-bottom">In</th>
                  <th className="w-16 px-2 py-3 text-right align-bottom">Out</th>
                  <th className="w-16 px-2 py-3 text-right align-bottom">Total</th>
                  <th className="w-16 px-2 py-3 text-right align-bottom">Cost</th>
                  <th className="w-16 px-2 py-3 text-center align-bottom leading-tight">
                    Thread
                    <br />
                    Cost
                  </th>
                  <th className="w-28 px-2 py-3 text-center align-bottom">Model</th>
                </tr>
              </thead>
              <tbody className="divide-y divide-gray-100">
                {activity.map((entry, idx) => {
                  const isSuccess = entry.status === "success";
                  const isFailed = entry.status === "failed";
                  const timestamp = new Date(entry.timestamp).toLocaleTimeString();
                  const totalTokens = entry.input_tokens + entry.output_tokens;

                  return (
                    <tr
                      key={entry.id || idx}
                      onClick={() => setSelectedEntry(entry)}
                      className={`hover:bg-gray-50 cursor-pointer transition-colors ${
                        isFailed ? "bg-red-50/30" : ""
                      }`}
                    >
                      {/* Status dot */}
                      <td className="px-2 py-2">
                        <span
                          className={`w-2 h-2 rounded-full inline-block ${
                            isSuccess ? "bg-green-500" : "bg-red-500"
                          }`}
                        ></span>
                      </td>

                      {/* Time */}
                      <td className="px-2 py-2 text-xs text-gray-500 font-mono whitespace-nowrap">
                        {timestamp}
                      </td>

                      {/* Tenant */}
                      <td className="px-2 py-2 text-sm text-gray-600">
                        <code className="text-xs bg-gray-100 px-1.5 py-0.5 rounded">
                          {entry.tenant}
                        </code>
                      </td>

                      {/* Content */}
                      <td
                        className="px-2 py-2 text-sm text-gray-800 truncate max-w-xs"
                        title={entry.content}
                      >
                        {entry.content}
                      </td>

                      {/* Duration */}
                      <td className="px-2 py-2 text-xs text-right whitespace-nowrap text-gray-500">
                        {(entry.processing_time_ms / 1000).toFixed(1)}s
                      </td>

                      {/* Input tokens */}
                      <td className="px-2 py-2 text-xs text-right font-mono text-gray-500 whitespace-nowrap">
                        {formatTokens(entry.input_tokens)}
                      </td>

                      {/* Output tokens */}
                      <td className="px-2 py-2 text-xs text-right font-mono text-gray-500 whitespace-nowrap">
                        {formatTokens(entry.output_tokens)}
                      </td>

                      {/* Total tokens */}
                      <td className="px-2 py-2 text-xs text-right font-mono text-purple-600 whitespace-nowrap">
                        {formatTokens(totalTokens)}
                      </td>

                      {/* Cost */}
                      <td className="px-2 py-2 text-xs text-right font-mono text-green-600 whitespace-nowrap">
                        {entry.cost_usd > 0 ? `$${entry.cost_usd.toFixed(4)}` : "-"}
                      </td>

                      {/* Thread Cost */}
                      <td className="px-2 py-2 text-xs text-center font-mono text-blue-600 whitespace-nowrap">
                        {entry.thread_cost_usd > 0 ? `$${entry.thread_cost_usd.toFixed(4)}` : "-"}
                      </td>

                      {/* Model/Provider */}
                      <td className="px-2 py-2 text-center">
                        {entry.provider && (
                          <span
                            className={`text-xs px-1.5 py-0.5 rounded inline-flex items-center gap-1 ${getProviderColor(
                              entry.provider
                            )}`}
                          >
                            <ProviderLogo provider={entry.provider} />
                            <span className="font-mono">{getModelName(entry.model)}</span>
                          </span>
                        )}
                      </td>
                    </tr>
                  );
                })}
              </tbody>
            </table>
          )}
        </div>
      </div>

      {/* Content Modal */}
      {selectedEntry && <ContentModal entry={selectedEntry} onClose={() => setSelectedEntry(null)} />}
    </div>
  );
}

// Content Modal - shows full message content
function ContentModal({ entry, onClose }: { entry: ActivityEntry; onClose: () => void }) {
  // Close on escape key
  useEffect(() => {
    const handleEsc = (e: KeyboardEvent) => {
      if (e.key === "Escape") onClose();
    };
    window.addEventListener("keydown", handleEsc);
    return () => window.removeEventListener("keydown", handleEsc);
  }, [onClose]);

  // Close on backdrop click
  const handleBackdropClick = (e: React.MouseEvent<HTMLDivElement>) => {
    if (e.target === e.currentTarget) onClose();
  };

  const formatTimestamp = (ts: string) => {
    if (!ts) return "N/A";
    return new Date(ts).toLocaleString();
  };

  return (
    <div
      className="fixed inset-0 bg-black/50 flex items-center justify-center z-50 p-4"
      onClick={handleBackdropClick}
    >
      <div className="bg-white rounded-lg shadow-xl w-full max-w-4xl max-h-[90vh] flex flex-col">
        {/* Header */}
        <div className="px-6 py-4 border-b border-gray-200 flex items-center justify-between">
          <h2 className="text-lg font-semibold text-gray-800">Message Content</h2>
          <button
            onClick={onClose}
            className="p-2 text-gray-400 hover:text-gray-600 transition-colors"
          >
            <CloseIcon />
          </button>
        </div>

        {/* Content */}
        <div className="flex-1 overflow-hidden flex flex-col">
          {/* Basic Details */}
          <div className="px-6 py-4 bg-gray-50 border-b border-gray-200">
            <div className="grid grid-cols-2 md:grid-cols-4 gap-4 text-sm">
              <div>
                <span className="text-gray-500">Tenant:</span>
                <div className="font-medium text-gray-800">{entry.tenant}</div>
              </div>
              <div>
                <span className="text-gray-500">User:</span>
                <div className="font-medium text-gray-800">{entry.user_id}</div>
              </div>
              <div>
                <span className="text-gray-500">Provider:</span>
                <div className="font-medium text-gray-800">{entry.provider || "N/A"}</div>
              </div>
              <div>
                <span className="text-gray-500">Model:</span>
                <div className="font-medium text-gray-800 font-mono text-xs">
                  {entry.model || "N/A"}
                </div>
              </div>
              <div>
                <span className="text-gray-500">Time:</span>
                <div className="font-medium text-gray-800">{formatTimestamp(entry.timestamp)}</div>
              </div>
              <div>
                <span className="text-gray-500">Duration:</span>
                <div className="font-medium text-gray-800">
                  {entry.processing_time_ms ? `${(entry.processing_time_ms / 1000).toFixed(1)}s` : "N/A"}
                </div>
              </div>
              <div>
                <span className="text-gray-500">Tokens In/Out:</span>
                <div className="font-medium text-gray-800">
                  {entry.input_tokens || 0} / {entry.output_tokens || 0}
                </div>
              </div>
              <div>
                <span className="text-gray-500">Cost:</span>
                <div className="font-medium text-green-600">
                  {entry.cost_usd > 0 ? `$${entry.cost_usd.toFixed(4)}` : "-"}
                </div>
              </div>
            </div>
            <div className="mt-3">
              <span className="text-gray-500 text-sm">Thread ID:</span>
              <div className="font-medium text-gray-800 font-mono text-xs">
                {entry.thread_id || "N/A"}
              </div>
            </div>
          </div>

          {/* Full Content */}
          <div className="flex-1 overflow-auto p-6">
            <h3 className="text-sm font-medium text-gray-600 mb-3">Response Content</h3>
            <pre className="bg-gray-100 rounded p-4 text-sm whitespace-pre-wrap font-mono overflow-x-auto">
              {entry.full_content || entry.content || "(No content)"}
            </pre>
          </div>
        </div>

        {/* Footer */}
        <div className="px-6 py-3 border-t border-gray-200 bg-gray-50 flex justify-end">
          <button
            onClick={onClose}
            className="px-4 py-2 bg-gray-200 text-gray-700 rounded hover:bg-gray-300 transition-colors"
          >
            Close
          </button>
        </div>
      </div>
    </div>
  );
}

// Icons
function CloseIcon() {
  return (
    <svg className="w-5 h-5" fill="none" viewBox="0 0 24 24" stroke="currentColor">
      <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M6 18L18 6M6 6l12 12" />
    </svg>
  );
}

// Provider logo for activity table
function ProviderLogo({ provider }: { provider: string }) {
  const p = provider?.toLowerCase() || "";

  if (p === "anthropic") {
    return (
      <svg className="w-3 h-3" viewBox="0 0 24 24" fill="currentColor">
        <path d="M12 2L14.5 9.5L22 12L14.5 14.5L12 22L9.5 14.5L2 12L9.5 9.5L12 2Z" />
      </svg>
    );
  }

  if (p === "gemini") {
    return (
      <svg className="w-3 h-3" viewBox="0 0 24 24" fill="currentColor">
        <path d="M8 2L9.5 6.5L14 8L9.5 9.5L8 14L6.5 9.5L2 8L6.5 6.5L8 2Z" />
        <path d="M16 10L17.5 14.5L22 16L17.5 17.5L16 22L14.5 17.5L10 16L14.5 14.5L16 10Z" />
      </svg>
    );
  }

  // OpenAI
  return (
    <svg className="w-3 h-3" viewBox="0 0 24 24" fill="currentColor">
      <path d="M12 2L21 7V17L12 22L3 17V7L12 2Z" />
    </svg>
  );
}
