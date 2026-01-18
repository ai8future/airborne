"use client";

import { useState } from "react";

interface TestResponse {
  reply: string;
  provider: string;
  model: string;
  input_tokens: number;
  output_tokens: number;
  processing_ms: number;
  error?: string;
}

const PROVIDERS = [
  { value: "gemini", label: "Gemini" },
  { value: "openai", label: "OpenAI" },
  { value: "anthropic", label: "Anthropic" },
];

export default function TestPanel() {
  const [prompt, setPrompt] = useState("Hello! What's 2 + 2?");
  const [provider, setProvider] = useState("gemini");
  const [loading, setLoading] = useState(false);
  const [response, setResponse] = useState<TestResponse | null>(null);

  const sendTestMessage = async () => {
    setLoading(true);
    setResponse(null);

    try {
      const res = await fetch("/api/test", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ prompt, provider }),
      });

      const data = await res.json();
      setResponse(data);
    } catch (error) {
      setResponse({
        reply: "",
        provider: "",
        model: "",
        input_tokens: 0,
        output_tokens: 0,
        processing_ms: 0,
        error: error instanceof Error ? error.message : "Unknown error",
      });
    } finally {
      setLoading(false);
    }
  };

  const getProviderColor = (p: string): string => {
    const pLower = p?.toLowerCase();
    if (pLower === "gemini") return "bg-cyan-100 text-cyan-700";
    if (pLower === "anthropic") return "bg-amber-100 text-amber-700";
    return "bg-emerald-100 text-emerald-700";
  };

  return (
    <div className="bg-white rounded-lg border border-gray-200 shadow-sm">
      <div className="px-4 py-3 border-b border-gray-200">
        <h3 className="font-semibold text-gray-800">Test AI Connection</h3>
        <p className="text-sm text-gray-500 mt-1">
          Send a test message to verify live connectivity
        </p>
      </div>

      <div className="p-4 space-y-4">
        {/* Provider selector */}
        <div>
          <label className="block text-sm font-medium text-gray-700 mb-1">
            Provider
          </label>
          <div className="flex gap-2">
            {PROVIDERS.map((p) => (
              <button
                key={p.value}
                onClick={() => setProvider(p.value)}
                className={`px-3 py-1.5 rounded-md text-sm font-medium transition-colors ${
                  provider === p.value
                    ? getProviderColor(p.value)
                    : "bg-gray-100 text-gray-600 hover:bg-gray-200"
                }`}
              >
                {p.label}
              </button>
            ))}
          </div>
        </div>

        {/* Prompt input */}
        <div>
          <label className="block text-sm font-medium text-gray-700 mb-1">
            Test Prompt
          </label>
          <textarea
            value={prompt}
            onChange={(e) => setPrompt(e.target.value)}
            rows={2}
            className="w-full px-3 py-2 border border-gray-300 rounded-md text-sm focus:outline-none focus:ring-2 focus:ring-blue-500 focus:border-blue-500"
            placeholder="Enter a test message..."
          />
        </div>

        {/* Send button */}
        <button
          onClick={sendTestMessage}
          disabled={loading || !prompt.trim()}
          className="w-full px-4 py-2 bg-blue-600 text-white rounded-md text-sm font-medium hover:bg-blue-700 disabled:bg-gray-300 disabled:cursor-not-allowed transition-colors flex items-center justify-center gap-2"
        >
          {loading ? (
            <>
              <svg
                className="animate-spin h-4 w-4"
                xmlns="http://www.w3.org/2000/svg"
                fill="none"
                viewBox="0 0 24 24"
              >
                <circle
                  className="opacity-25"
                  cx="12"
                  cy="12"
                  r="10"
                  stroke="currentColor"
                  strokeWidth="4"
                />
                <path
                  className="opacity-75"
                  fill="currentColor"
                  d="M4 12a8 8 0 018-8V0C5.373 0 0 5.373 0 12h4zm2 5.291A7.962 7.962 0 014 12H0c0 3.042 1.135 5.824 3 7.938l3-2.647z"
                />
              </svg>
              Sending...
            </>
          ) : (
            "Send Test Message"
          )}
        </button>

        {/* Response */}
        {response && (
          <div
            className={`p-4 rounded-md ${
              response.error
                ? "bg-red-50 border border-red-200"
                : "bg-green-50 border border-green-200"
            }`}
          >
            {response.error ? (
              <div>
                <p className="text-sm font-medium text-red-800">Error</p>
                <p className="text-sm text-red-600 mt-1">{response.error}</p>
              </div>
            ) : (
              <div className="space-y-3">
                {/* Metadata row */}
                <div className="flex flex-wrap gap-2 text-xs">
                  <span className={`px-2 py-0.5 rounded ${getProviderColor(response.provider)}`}>
                    {response.provider}
                  </span>
                  <span className="px-2 py-0.5 rounded bg-gray-100 text-gray-600">
                    {response.model}
                  </span>
                  <span className="px-2 py-0.5 rounded bg-gray-100 text-gray-600">
                    {response.processing_ms}ms
                  </span>
                  <span className="px-2 py-0.5 rounded bg-purple-100 text-purple-700">
                    {response.input_tokens} in / {response.output_tokens} out
                  </span>
                </div>

                {/* Reply */}
                <div>
                  <p className="text-sm font-medium text-gray-700">Response:</p>
                  <p className="text-sm text-gray-800 mt-1 whitespace-pre-wrap">
                    {response.reply}
                  </p>
                </div>
              </div>
            )}
          </div>
        )}
      </div>
    </div>
  );
}
