"use client";

import { useState, useEffect, useMemo } from "react";

interface DebugData {
  message_id: string;
  thread_id: string;
  tenant_id: string;
  user_id: string;
  timestamp: string;

  // Request
  system_prompt: string;
  user_input: string;
  request_model: string;
  request_provider: string;
  request_timestamp: string;

  // Response
  response_text: string;
  response_model: string;
  tokens_in: number;
  tokens_out: number;
  cost_usd: number;
  grounding_queries?: number;
  grounding_cost_usd?: number;
  duration_ms: number;
  response_id?: string;
  citations?: string;

  // Raw JSON
  raw_request_json?: string;
  raw_response_json?: string;

  // Status
  status: string;
  error?: string;
}

// Citation structure from Gemini's groundingChunks
interface Citation {
  type: "url" | "file";
  uri?: string;
  title?: string;
  filename?: string;
  snippet?: string;
  start_index?: number;
  end_index?: number;
}

// Parse citations from JSON string
function parseCitations(citationsStr: string | undefined): Citation[] {
  if (!citationsStr || citationsStr === "") return [];
  try {
    const parsed = JSON.parse(citationsStr);
    if (Array.isArray(parsed)) {
      return parsed as Citation[];
    }
    return [];
  } catch {
    return [];
  }
}

// Format citation as display component
function CitationsList({ citations }: { citations: Citation[] }) {
  if (citations.length === 0) return null;

  const webCitations = citations.filter(c => c.type === "url" && c.uri);
  const fileCitations = citations.filter(c => c.type === "file" && c.filename);

  return (
    <div className="space-y-3">
      {webCitations.length > 0 && (
        <div>
          <h5 className="text-xs font-medium text-gray-500 mb-2">Web Sources</h5>
          <ol className="list-decimal list-inside space-y-1.5">
            {webCitations.map((citation, idx) => (
              <li key={idx} className="text-sm">
                <a
                  href={citation.uri}
                  target="_blank"
                  rel="noopener noreferrer"
                  className="text-blue-600 hover:text-blue-800 hover:underline"
                >
                  {citation.title || new URL(citation.uri!).hostname}
                </a>
                <span className="text-gray-400 text-xs ml-2">
                  {citation.uri && new URL(citation.uri).hostname}
                </span>
              </li>
            ))}
          </ol>
        </div>
      )}
      {fileCitations.length > 0 && (
        <div>
          <h5 className="text-xs font-medium text-gray-500 mb-2">File Sources</h5>
          <ol className="list-decimal list-inside space-y-1.5">
            {fileCitations.map((citation, idx) => (
              <li key={idx} className="text-sm text-gray-700">
                <span className="font-mono text-xs bg-gray-100 px-1.5 py-0.5 rounded">
                  {citation.filename}
                </span>
                {citation.snippet && (
                  <p className="mt-1 ml-4 text-xs text-gray-500 italic truncate max-w-md">
                    {citation.snippet}
                  </p>
                )}
              </li>
            ))}
          </ol>
        </div>
      )}
    </div>
  );
}

interface DebugModalProps {
  messageId: string;
  onClose: () => void;
}

export default function DebugModal({ messageId, onClose }: DebugModalProps) {
  const [debugData, setDebugData] = useState<DebugData | null>(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [viewMode, setViewMode] = useState<"parsed" | "json">("parsed");

  useEffect(() => {
    const fetchDebugData = async () => {
      try {
        const res = await fetch(`/api/debug/${messageId}`);
        if (!res.ok) {
          if (res.status === 404) {
            setError("Debug data not found.");
          } else {
            setError(`Failed to fetch debug data: ${res.statusText}`);
          }
          return;
        }
        const data = await res.json();
        if (data.error) {
          setError(data.error);
        } else {
          setDebugData(data);
        }
      } catch (err) {
        setError(`Error loading debug data: ${err instanceof Error ? err.message : "Unknown error"}`);
      } finally {
        setLoading(false);
      }
    };

    fetchDebugData();
  }, [messageId]);

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

  // Pretty-print JSON
  const formatJSON = (jsonStr?: string) => {
    if (!jsonStr) return null;
    try {
      const parsed = JSON.parse(jsonStr);
      return JSON.stringify(parsed, null, 2);
    } catch {
      return jsonStr;
    }
  };

  const formattedRequestJSON = useMemo(
    () => formatJSON(debugData?.raw_request_json),
    [debugData?.raw_request_json]
  );
  const formattedResponseJSON = useMemo(
    () => formatJSON(debugData?.raw_response_json),
    [debugData?.raw_response_json]
  );

  const hasJSON = debugData?.raw_request_json || debugData?.raw_response_json;

  return (
    <div
      className="fixed inset-0 bg-black/50 flex items-center justify-center z-50 p-4"
      onClick={handleBackdropClick}
    >
      <div className="bg-white rounded-lg shadow-xl w-full max-w-7xl max-h-[90vh] flex flex-col">
        {/* Header */}
        <div className="px-6 py-4 border-b border-gray-200 flex items-center justify-between">
          <div className="flex items-center gap-4">
            <h2 className="text-lg font-semibold text-gray-800">AI Request/Response Inspector</h2>
            {debugData && (
              <div className="flex bg-gray-100 rounded-lg p-0.5">
                <button
                  onClick={() => setViewMode("parsed")}
                  className={`px-3 py-1 text-sm rounded-md transition-colors ${
                    viewMode === "parsed"
                      ? "bg-white text-gray-800 shadow-sm"
                      : "text-gray-500 hover:text-gray-700"
                  }`}
                >
                  Parsed
                </button>
                <button
                  onClick={() => setViewMode("json")}
                  disabled={!hasJSON}
                  className={`px-3 py-1 text-sm rounded-md transition-colors ${
                    viewMode === "json"
                      ? "bg-white text-gray-800 shadow-sm"
                      : "text-gray-500 hover:text-gray-700"
                  } ${!hasJSON ? "opacity-50 cursor-not-allowed" : ""}`}
                  title={!hasJSON ? "Raw JSON not available for this request" : ""}
                >
                  JSON
                </button>
              </div>
            )}
          </div>
          <button
            onClick={onClose}
            className="p-2 text-gray-400 hover:text-gray-600 transition-colors"
          >
            <CloseIcon />
          </button>
        </div>

        {/* Content */}
        <div className="flex-1 overflow-hidden flex flex-col">
          {loading ? (
            <div className="flex-1 flex items-center justify-center">
              <div className="text-gray-500">
                <SpinnerIcon className="w-8 h-8 animate-spin mx-auto mb-2" />
                Loading debug data...
              </div>
            </div>
          ) : error ? (
            <div className="flex-1 flex items-center justify-center">
              <div className="text-red-500 text-center">
                <AlertIcon className="w-8 h-8 mx-auto mb-2" />
                {error}
              </div>
            </div>
          ) : debugData ? (
            <>
              {/* Basic Details */}
              <div className="px-6 py-4 bg-gray-50 border-b border-gray-200">
                <div className="grid grid-cols-2 md:grid-cols-4 gap-4 text-sm">
                  <div>
                    <span className="text-gray-500">Tenant:</span>
                    <div className="font-medium text-gray-800">{debugData.tenant_id}</div>
                  </div>
                  <div>
                    <span className="text-gray-500">User:</span>
                    <div className="font-medium text-gray-800">{debugData.user_id}</div>
                  </div>
                  <div>
                    <span className="text-gray-500">Provider:</span>
                    <div className="font-medium text-gray-800">{debugData.request_provider || "N/A"}</div>
                  </div>
                  <div>
                    <span className="text-gray-500">Model:</span>
                    <div className="font-medium text-gray-800 font-mono text-xs">
                      {debugData.response_model || "N/A"}
                    </div>
                  </div>
                  <div>
                    <span className="text-gray-500">Time:</span>
                    <div className="font-medium text-gray-800">{formatTimestamp(debugData.timestamp)}</div>
                  </div>
                  <div>
                    <span className="text-gray-500">Duration:</span>
                    <div className="font-medium text-gray-800">
                      {debugData.duration_ms ? `${(debugData.duration_ms / 1000).toFixed(1)}s` : "N/A"}
                    </div>
                  </div>
                  <div>
                    <span className="text-gray-500">Tokens In/Out:</span>
                    <div className="font-medium text-gray-800">
                      {debugData.tokens_in || 0} / {debugData.tokens_out || 0}
                    </div>
                  </div>
                  <div>
                    <span className="text-gray-500">Grounding:</span>
                    <div className="font-medium text-gray-800">
                      {debugData.grounding_queries && debugData.grounding_queries > 0
                        ? `${debugData.grounding_queries} queries`
                        : "None"}
                    </div>
                  </div>
                </div>
                {/* Cost Breakdown */}
                <div className="mt-4 p-3 bg-white rounded-lg border border-gray-200">
                  <h4 className="text-sm font-medium text-gray-600 mb-2">Cost Breakdown</h4>
                  <div className="grid grid-cols-3 gap-4 text-sm">
                    <div>
                      <span className="text-gray-500">Token Cost:</span>
                      <div className="font-medium text-gray-800">
                        ${((debugData.cost_usd || 0) - (debugData.grounding_cost_usd || 0)).toFixed(4)}
                      </div>
                    </div>
                    <div>
                      <span className="text-gray-500">Grounding Cost:</span>
                      <div className="font-medium text-gray-800">
                        {debugData.grounding_cost_usd && debugData.grounding_cost_usd > 0
                          ? `$${debugData.grounding_cost_usd.toFixed(4)}`
                          : "$0.0000"}
                      </div>
                    </div>
                    <div>
                      <span className="text-gray-500">Total Cost:</span>
                      <div className="font-medium text-green-600">
                        ${(debugData.cost_usd || 0).toFixed(4)}
                      </div>
                    </div>
                  </div>
                </div>
                <div className="mt-3">
                  <span className="text-gray-500 text-sm">Thread ID:</span>
                  <div className="font-medium text-gray-800 font-mono text-xs">
                    {debugData.thread_id || "N/A"}
                  </div>
                </div>
              </div>

              {/* Content panels */}
              {viewMode === "json" ? (
                /* JSON view: Vertical stack */
                <div className="flex-1 overflow-auto p-4 space-y-6">
                  {/* Request JSON */}
                  <div>
                    <h3 className="font-semibold text-blue-800 mb-3">Request (Raw HTTP Body)</h3>
                    <pre className="bg-gray-900 text-green-400 rounded p-4 text-xs font-mono overflow-x-auto whitespace-pre">
                      {formattedRequestJSON || "(No raw JSON captured)"}
                    </pre>
                  </div>

                  {/* Response JSON */}
                  <div>
                    <h3 className="font-semibold text-green-800 mb-3">Response (Raw HTTP Body)</h3>
                    <pre className="bg-gray-900 text-green-400 rounded p-4 text-xs font-mono overflow-x-auto whitespace-pre">
                      {formattedResponseJSON || "(No raw JSON captured)"}
                    </pre>
                  </div>
                </div>
              ) : (
                /* Parsed view: Side-by-side panels */
                <div className="flex-1 flex overflow-hidden">
                  {/* Left: Request */}
                  <div className="flex-1 flex flex-col border-r border-gray-200">
                    <div className="px-4 py-2 bg-blue-50 border-b border-gray-200">
                      <h3 className="font-semibold text-blue-800">Request (Sent to AI)</h3>
                    </div>
                    <div className="flex-1 overflow-auto p-4">
                      <div className="space-y-4">
                        {/* System Prompt */}
                        <div>
                          <h4 className="text-sm font-medium text-gray-600 mb-2">System Prompt</h4>
                          <pre className="bg-gray-100 rounded p-3 text-sm whitespace-pre-wrap font-mono overflow-x-auto max-h-48 overflow-y-auto">
                            {debugData.system_prompt || "(No system prompt)"}
                          </pre>
                        </div>

                        {/* User Input */}
                        <div>
                          <h4 className="text-sm font-medium text-gray-600 mb-2">User Input</h4>
                          <pre className="bg-gray-100 rounded p-3 text-sm whitespace-pre-wrap font-mono overflow-x-auto">
                            {debugData.user_input || "(No user input)"}
                          </pre>
                        </div>
                      </div>
                    </div>
                  </div>

                  {/* Right: Response */}
                  <div className="flex-1 flex flex-col">
                    <div className="px-4 py-2 bg-green-50 border-b border-gray-200">
                      <h3 className="font-semibold text-green-800">Response (From AI)</h3>
                    </div>
                    <div className="flex-1 overflow-auto p-4">
                      <div className="space-y-4">
                        {/* Response Text */}
                        <div>
                          <h4 className="text-sm font-medium text-gray-600 mb-2">Response Text</h4>
                          <pre className="bg-gray-100 rounded p-3 text-sm whitespace-pre-wrap font-mono overflow-x-auto">
                            {debugData.response_text || "(No response)"}
                          </pre>
                        </div>

                        {/* Citations if present */}
                        {debugData.citations && debugData.citations !== "" && (
                          <div>
                            <h4 className="text-sm font-medium text-gray-600 mb-2">Sources</h4>
                            <div className="bg-gray-50 rounded-lg p-4 border border-gray-200">
                              <CitationsList citations={parseCitations(debugData.citations)} />
                            </div>
                          </div>
                        )}

                        {/* Error if present */}
                        {debugData.error && (
                          <div>
                            <h4 className="text-sm font-medium text-red-600 mb-2">Error</h4>
                            <pre className="bg-red-50 border border-red-200 rounded p-3 text-sm text-red-700 whitespace-pre-wrap">
                              {debugData.error}
                            </pre>
                          </div>
                        )}
                      </div>
                    </div>
                  </div>
                </div>
              )}
            </>
          ) : (
            <div className="flex-1 flex items-center justify-center text-gray-500">
              No debug data available
            </div>
          )}
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

function SpinnerIcon({ className }: { className?: string }) {
  return (
    <svg className={className} fill="none" viewBox="0 0 24 24">
      <circle className="opacity-25" cx="12" cy="12" r="10" stroke="currentColor" strokeWidth="4"></circle>
      <path
        className="opacity-75"
        fill="currentColor"
        d="M4 12a8 8 0 018-8V0C5.373 0 0 5.373 0 12h4zm2 5.291A7.962 7.962 0 014 12H0c0 3.042 1.135 5.824 3 7.938l3-2.647z"
      ></path>
    </svg>
  );
}

function AlertIcon({ className }: { className?: string }) {
  return (
    <svg className={className || "w-5 h-5"} fill="none" viewBox="0 0 24 24" stroke="currentColor">
      <path
        strokeLinecap="round"
        strokeLinejoin="round"
        strokeWidth={2}
        d="M12 9v2m0 4h.01m-6.938 4h13.856c1.54 0 2.502-1.667 1.732-3L13.732 4c-.77-1.333-2.694-1.333-3.464 0L3.34 16c-.77 1.333.192 3 1.732 3z"
      />
    </svg>
  );
}
