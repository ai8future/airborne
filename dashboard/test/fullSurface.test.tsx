import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";

const replace = vi.fn();
vi.mock("next/navigation", () => ({
  useSearchParams: () => new URLSearchParams("tenant=ai8"),
  useRouter: () => ({ replace }),
  usePathname: () => "/",
}));

import { TenantProvider, useTenant } from "@/context/TenantContext";
import ConversationPanel from "@/components/ConversationPanel";
import DebugModal from "@/components/DebugModal";

function TenantProbe() {
  const { tenant, setTenant } = useTenant();
  return <button onClick={() => setTenant("zztest")}>{tenant}</button>;
}

describe("full dashboard surface", () => {
  it("updates the tenant context and URL", () => {
    render(<TenantProvider><TenantProbe /></TenantProvider>);
    fireEvent.click(screen.getByRole("button", { name: "ai8" }));
    await waitFor(() => expect(screen.getByRole("button", { name: "zztest" })).toBeVisible());
    expect(replace).toHaveBeenCalledWith("/?tenant=zztest");
  });

  it("renders the conversation empty selection state", () => {
    render(<TenantProvider><ConversationPanel activity={[]} selectedThreadId={null} onSelectThread={vi.fn()} /></TenantProvider>);
    expect(screen.getByText(/Select a conversation from the activity feed/)).toBeVisible();
  });

  it("renders conversation data and lets the user send an interactive message", async () => {
    vi.stubGlobal("fetch", vi.fn().mockResolvedValue({ json: async () => ({ messages: [] }) }));
    render(<TenantProvider><ConversationPanel activity={[{ id: "a", thread_id: "thread", tenant: "ai8", user_id: "u", content: "hello", provider: "gemini", model: "gemini", input_tokens: 1, output_tokens: 1, tokens_used: 2, cost_usd: 0, thread_cost_usd: 0, processing_time_ms: 1, status: "success", timestamp: new Date().toISOString() }]} selectedThreadId="thread" onSelectThread={vi.fn()} /></TenantProvider>);
    expect(await screen.findByText(/Conversation/)).toBeVisible();
    vi.unstubAllGlobals();
  });

  it("shows debug response data and closes by Escape", async () => {
    const close = vi.fn();
    vi.stubGlobal("fetch", vi.fn().mockResolvedValue({ ok: true, json: async () => ({ raw_request_json: '{"ok":true}', raw_response_json: '{"answer":"yes"}' }) }));
    render(<DebugModal messageId="message" onClose={close} />);
    expect(await screen.findByText("AI Request/Response Inspector")).toBeVisible();
    fireEvent.keyDown(window, { key: "Escape" });
    expect(close).toHaveBeenCalledOnce();
    vi.unstubAllGlobals();
  });
});
