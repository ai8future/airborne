"use client";

import { useState } from "react";
import DebugModal from "./DebugModal";

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

interface ActivityPanelProps {
  activity: ActivityEntry[];
  loading: boolean;
  error: string | null;
  paused: boolean;
  onPauseToggle: () => void;
  onClear: () => void;
}

export default function ActivityPanel({
  activity,
  loading,
  error,
  paused,
  onPauseToggle,
  onClear,
}: ActivityPanelProps) {
  const [debugMessageId, setDebugMessageId] = useState<string | null>(null);

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
              onClick={onPauseToggle}
              className={`px-3 py-1.5 rounded text-sm font-medium transition-colors ${
                paused
                  ? "bg-green-100 text-green-700 hover:bg-green-200"
                  : "bg-gray-100 text-gray-700 hover:bg-gray-200"
              }`}
            >
              {paused ? "Resume" : "Pause"}
            </button>
            <button
              onClick={onClear}
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
                <tr className="text-xs text-gray-500 uppercase tracking-wider whitespace-nowrap">
                  <th className="w-6 px-2 py-1.5"></th>
                  <th className="w-20 px-2 py-1.5 text-left">Time</th>
                  <th className="w-24 px-2 py-1.5 text-left">Tenant</th>
                  <th className="px-2 py-1.5 text-left">Content</th>
                  <th className="w-12 px-2 py-1.5 text-right">Dur</th>
                  <th className="w-14 px-2 py-1.5 text-right">In</th>
                  <th className="w-14 px-2 py-1.5 text-right">Out</th>
                  <th className="w-14 px-2 py-1.5 text-right">Total</th>
                  <th className="w-14 px-2 py-1.5 text-right">Cost</th>
                  <th className="w-12 px-2 py-1.5 text-right" title="Thread Cost">Thr$</th>
                  <th className="w-32 px-2 py-1.5 text-center">Model</th>
                </tr>
              </thead>
              <tbody className="divide-y divide-gray-100">
                {[...activity].sort((a, b) => new Date(b.timestamp).getTime() - new Date(a.timestamp).getTime()).map((entry, idx) => {
                  const isSuccess = entry.status === "success";
                  const isFailed = entry.status === "failed";
                  const timestamp = new Date(entry.timestamp).toLocaleTimeString();
                  const totalTokens = entry.input_tokens + entry.output_tokens;
                  const durationSec = entry.processing_time_ms ? (entry.processing_time_ms / 1000).toFixed(1) : "-";

                  return (
                    <tr
                      key={entry.id || idx}
                      onClick={() => setDebugMessageId(entry.id)}
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
                        {durationSec !== "-" ? `${durationSec}s` : "-"}
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

      {/* Debug Modal (side-by-side request/response inspector) */}
      {debugMessageId && <DebugModal messageId={debugMessageId} onClose={() => setDebugMessageId(null)} />}
    </div>
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
