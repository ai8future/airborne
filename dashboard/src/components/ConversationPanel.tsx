"use client";

import { useState, useEffect, useRef, useCallback } from "react";
import ReactMarkdown from "react-markdown";

interface ThreadMessage {
  id: string;
  role: "user" | "assistant";
  content: string;
  timestamp: string;
  provider?: string;
  model?: string;
  tokens_in?: number;
  tokens_out?: number;
  cost_usd?: number;
}

interface Thread {
  thread_id: string;
  tenant: string;
  last_message: string;
  last_timestamp: string;
  message_count: number;
  total_cost: number;
}

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

interface ConversationPanelProps {
  activity: ActivityEntry[];
}

type ViewMode = "formatted" | "raw" | "json";

// Message bubble component with view mode toggle
function MessageBubble({ message }: { message: ThreadMessage }) {
  const [viewMode, setViewMode] = useState<ViewMode>("formatted");
  const [jsonData, setJsonData] = useState<string | null>(null);
  const [loadingJson, setLoadingJson] = useState(false);

  const formatTime = (timestamp: string) => {
    return new Date(timestamp).toLocaleTimeString([], { hour: "2-digit", minute: "2-digit" });
  };

  // Fetch JSON data from debug endpoint when JSON view is requested
  const fetchJsonData = async () => {
    if (jsonData) return; // Already fetched
    setLoadingJson(true);
    try {
      const res = await fetch(`/api/debug/${message.id}`);
      const data = await res.json();
      if (data.response_json) {
        setJsonData(JSON.stringify(JSON.parse(data.response_json), null, 2));
      } else {
        // Construct a representative JSON from available data
        const constructedJson = {
          provider: message.provider,
          model: message.model,
          content: message.content,
          usage: {
            input_tokens: message.tokens_in,
            output_tokens: message.tokens_out,
          },
          cost_usd: message.cost_usd,
        };
        setJsonData(JSON.stringify(constructedJson, null, 2));
      }
    } catch {
      // Fallback to constructed JSON
      const constructedJson = {
        provider: message.provider,
        model: message.model,
        content: message.content,
        usage: {
          input_tokens: message.tokens_in,
          output_tokens: message.tokens_out,
        },
        cost_usd: message.cost_usd,
      };
      setJsonData(JSON.stringify(constructedJson, null, 2));
    } finally {
      setLoadingJson(false);
    }
  };

  const handleViewChange = (mode: ViewMode) => {
    setViewMode(mode);
    if (mode === "json" && !jsonData) {
      fetchJsonData();
    }
  };

  const renderContent = () => {
    if (viewMode === "json") {
      if (loadingJson) {
        return <div className="text-xs text-slate-400">Loading...</div>;
      }
      return (
        <pre className="text-xs whitespace-pre-wrap leading-relaxed font-mono overflow-x-auto text-slate-700 bg-slate-50 p-3 rounded-lg">
          {jsonData || "No JSON data available"}
        </pre>
      );
    }
    if (viewMode === "raw") {
      return (
        <pre className="text-sm whitespace-pre-wrap leading-relaxed font-mono text-xs overflow-x-auto text-slate-700">
          {message.content}
        </pre>
      );
    }
    return (
      <div className="text-sm leading-relaxed prose prose-sm max-w-none prose-p:my-1 prose-headings:my-2 prose-ul:my-1 prose-ol:my-1 prose-li:my-0 text-slate-700">
        <ReactMarkdown>{message.content}</ReactMarkdown>
      </div>
    );
  };

  const ViewToggle = ({ className }: { className: string }) => (
    <div className={`flex items-center gap-3 mt-2 ${className}`}>
      <button
        onClick={() => handleViewChange("formatted")}
        className={`text-xs transition-colors ${viewMode === "formatted" ? "font-medium" : "opacity-60 hover:opacity-100"}`}
      >
        Formatted
      </button>
      <span className="text-xs opacity-30">|</span>
      <button
        onClick={() => handleViewChange("raw")}
        className={`text-xs transition-colors ${viewMode === "raw" ? "font-medium" : "opacity-60 hover:opacity-100"}`}
      >
        Raw
      </button>
      <span className="text-xs opacity-30">|</span>
      <button
        onClick={() => handleViewChange("json")}
        className={`text-xs transition-colors ${viewMode === "json" ? "font-medium" : "opacity-60 hover:opacity-100"}`}
      >
        JSON
      </button>
    </div>
  );

  // Assistant messages: centered, white background
  if (message.role === "assistant") {
    return (
      <div className="flex justify-center">
        <div className="w-full max-w-2xl">
          <div className="flex items-center gap-2 mb-1 justify-center">
            <span className="text-xs text-slate-400">{formatTime(message.timestamp)}</span>
            {message.provider && (
              <span className="text-xs text-slate-400">
                {message.provider}/{message.model}
              </span>
            )}
          </div>
          <div className="bg-white rounded-2xl px-5 py-4 shadow-sm border border-gray-100">
            {renderContent()}
            <ViewToggle className="text-slate-400" />
          </div>
        </div>
      </div>
    );
  }

  // User messages: right-aligned, light blue background
  return (
    <div className="flex justify-end">
      <div className="max-w-[70%]">
        <div className="flex items-center gap-2 mb-1 justify-end">
          <span className="text-xs text-slate-400">{formatTime(message.timestamp)}</span>
        </div>
        <div className="bg-blue-100 text-slate-800 rounded-2xl px-4 py-3 shadow-sm">
          {renderContent()}
          <ViewToggle className="text-blue-500" />
        </div>
      </div>
    </div>
  );
}

export default function ConversationPanel({ activity }: ConversationPanelProps) {
  const [selectedThreadId, setSelectedThreadId] = useState<string | null>(null);
  const [messages, setMessages] = useState<ThreadMessage[]>([]);
  const [loading, setLoading] = useState(false);
  const messagesEndRef = useRef<HTMLDivElement>(null);
  const activityRef = useRef(activity);

  // Keep activity ref updated
  useEffect(() => {
    activityRef.current = activity;
  }, [activity]);

  // Group activity by thread_id to create thread list
  const threads = activity.reduce((acc, entry) => {
    if (!entry.thread_id) return acc;

    if (!acc[entry.thread_id]) {
      acc[entry.thread_id] = {
        thread_id: entry.thread_id,
        tenant: entry.tenant,
        last_message: entry.content,
        last_timestamp: entry.timestamp,
        message_count: 1,
        total_cost: entry.thread_cost_usd || 0,
      };
    } else {
      acc[entry.thread_id].message_count++;
      // Update if this is a newer message
      if (new Date(entry.timestamp) > new Date(acc[entry.thread_id].last_timestamp)) {
        acc[entry.thread_id].last_message = entry.content;
        acc[entry.thread_id].last_timestamp = entry.timestamp;
        acc[entry.thread_id].total_cost = entry.thread_cost_usd || acc[entry.thread_id].total_cost;
      }
    }
    return acc;
  }, {} as Record<string, Thread>);

  const threadList = Object.values(threads).sort(
    (a, b) => new Date(b.last_timestamp).getTime() - new Date(a.last_timestamp).getTime()
  );

  // Fetch thread messages when a thread is selected - use ref to avoid re-fetching on activity changes
  const fetchThreadMessages = useCallback(async (threadId: string) => {
    setLoading(true);
    try {
      const res = await fetch(`/api/threads/${threadId}`);
      const data = await res.json();
      if (data.messages && data.messages.length > 0) {
        setMessages(data.messages);
      } else {
        // Fallback: construct messages from activity data
        const currentActivity = activityRef.current;
        const threadActivity = currentActivity
          .filter(a => a.thread_id === threadId)
          .sort((a, b) => new Date(a.timestamp).getTime() - new Date(b.timestamp).getTime());

        const fallbackMessages: ThreadMessage[] = [];
        threadActivity.forEach(entry => {
          // The activity entry contains the LLM response in content/full_content
          // We don't have the original user input, so just show the assistant response
          fallbackMessages.push({
            id: entry.id,
            role: "assistant",
            content: entry.full_content || entry.content || "...",
            timestamp: entry.timestamp,
            provider: entry.provider,
            model: entry.model,
            tokens_in: entry.input_tokens,
            tokens_out: entry.output_tokens,
            cost_usd: entry.cost_usd,
          });
        });
        setMessages(fallbackMessages);
      }
    } catch (error) {
      console.error("Failed to fetch thread messages:", error);
      setMessages([]);
    } finally {
      setLoading(false);
    }
  }, []); // No dependencies - uses ref for activity

  // Auto-select first thread if none selected
  useEffect(() => {
    if (!selectedThreadId && threadList.length > 0) {
      setSelectedThreadId(threadList[0].thread_id);
    }
  }, [selectedThreadId, threadList]);

  // Fetch messages when thread changes (only when selectedThreadId changes)
  useEffect(() => {
    if (selectedThreadId) {
      fetchThreadMessages(selectedThreadId);
    }
  }, [selectedThreadId, fetchThreadMessages]);

  // Scroll to bottom when messages change
  useEffect(() => {
    messagesEndRef.current?.scrollIntoView({ behavior: "smooth" });
  }, [messages]);

  const formatDate = (timestamp: string) => {
    const date = new Date(timestamp);
    const today = new Date();
    const formatTime = (ts: string) => new Date(ts).toLocaleTimeString([], { hour: "2-digit", minute: "2-digit" });
    if (date.toDateString() === today.toDateString()) {
      return formatTime(timestamp);
    }
    return date.toLocaleDateString([], { month: "short", day: "numeric" }) + " " + formatTime(timestamp);
  };

  return (
    <div className="bg-white rounded-lg border border-gray-200 shadow-sm overflow-hidden">
      <div className="px-4 py-3 border-b border-gray-200">
        <h3 className="font-semibold text-gray-800">Conversations</h3>
      </div>

      <div className="flex h-[600px]">
        {/* Thread list - left sidebar */}
        <div className="w-72 border-r border-gray-200 overflow-y-auto bg-gray-50">
          {threadList.length === 0 ? (
            <div className="p-4 text-center text-gray-500 text-sm">
              No conversations yet
            </div>
          ) : (
            threadList.map((thread) => (
              <button
                key={thread.thread_id}
                onClick={() => setSelectedThreadId(thread.thread_id)}
                className={`w-full p-3 text-left border-b border-gray-100 hover:bg-gray-100 transition-colors ${
                  selectedThreadId === thread.thread_id ? "bg-blue-50 border-l-2 border-l-blue-500" : ""
                }`}
              >
                <div className="flex items-center justify-between mb-1">
                  <code className="text-xs bg-gray-200 px-1.5 py-0.5 rounded text-gray-600">
                    {thread.tenant}
                  </code>
                  <span className="text-xs text-gray-400">{formatDate(thread.last_timestamp)}</span>
                </div>
                <p className="text-sm text-gray-800 truncate">{thread.last_message}</p>
                <div className="flex items-center gap-2 mt-1">
                  <span className="text-xs text-gray-400">{thread.message_count} msgs</span>
                  {thread.total_cost > 0 && (
                    <span className="text-xs text-green-600">${thread.total_cost.toFixed(4)}</span>
                  )}
                </div>
              </button>
            ))
          )}
        </div>

        {/* Messages area - right */}
        <div className="flex-1 flex flex-col bg-gradient-to-b from-slate-50 to-slate-100 relative">
          {/* Messages */}
          <div className="flex-1 overflow-y-auto p-4 pb-24">
            {loading ? (
              <div className="flex items-center justify-center h-full text-gray-400">
                Loading messages...
              </div>
            ) : messages.length === 0 ? (
              <div className="flex items-center justify-center h-full text-gray-400">
                {selectedThreadId ? "No messages in this thread" : "Select a conversation"}
              </div>
            ) : (
              <div className="space-y-4">
                {messages.map((message) => (
                  <MessageBubble key={message.id} message={message} />
                ))}
                <div ref={messagesEndRef} />
              </div>
            )}
          </div>

          {/* Chat input - centered at bottom */}
          <div className="absolute bottom-0 left-0 right-0 p-4 bg-gradient-to-t from-slate-100 to-transparent">
            <div className="max-w-2xl mx-auto">
              <div className="glass-input-container flex items-center gap-3 p-3 rounded-2xl">
                <textarea
                  placeholder="Ask anything..."
                  rows={1}
                  className="flex-1 resize-none min-h-[24px] max-h-[120px] leading-6 bg-transparent outline-none text-slate-800 placeholder:text-slate-400 text-sm"
                  style={{ height: '24px' }}
                  onInput={(e) => {
                    const target = e.target as HTMLTextAreaElement;
                    target.style.height = '24px';
                    target.style.height = Math.min(target.scrollHeight, 120) + 'px';
                  }}
                />
                <button
                  type="button"
                  className="size-9 flex items-center justify-center rounded-xl bg-slate-800 text-white hover:bg-blue-600 transition-colors"
                >
                  <svg className="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                    <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M12 19l9 2-9-18-9 18 9-2zm0 0v-8" />
                  </svg>
                </button>
              </div>
            </div>
          </div>
        </div>
      </div>
    </div>
  );
}
