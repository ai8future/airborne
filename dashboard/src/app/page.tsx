"use client";

import { useState, useEffect, useCallback } from "react";
import ActivityPanel from "@/components/ActivityPanel";
import ConversationPanel from "@/components/ConversationPanel";

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

export default function Home() {
  const [paused, setPaused] = useState(false);
  const [activity, setActivity] = useState<ActivityEntry[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);

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

  const handleClear = () => setActivity([]);

  return (
    <div className="space-y-6">
      <ActivityPanel
        activity={activity}
        loading={loading}
        error={error}
        paused={paused}
        onPauseToggle={() => setPaused(!paused)}
        onClear={handleClear}
      />
      <ConversationPanel activity={activity} />
    </div>
  );
}
