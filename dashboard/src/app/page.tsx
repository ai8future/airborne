"use client";

import ActivityPanel from "@/components/ActivityPanel";
import TestPanel from "@/components/TestPanel";

export default function Home() {
  return (
    <div className="space-y-6">
      <TestPanel />
      <ActivityPanel />
    </div>
  );
}
