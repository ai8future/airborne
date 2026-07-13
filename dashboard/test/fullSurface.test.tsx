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
import TenantSelector from "@/components/TenantSelector";

function TenantProbe() {
  const { tenant, setTenant } = useTenant();
  return <button onClick={() => setTenant("zztest")}>{tenant}</button>;
}

describe("full dashboard surface", () => {
  it("updates the tenant context and URL", async () => {
    render(<TenantProvider><TenantProbe /></TenantProvider>);
    fireEvent.click(screen.getByRole("button", { name: "ai8" }));
    await waitFor(() => expect(replace).toHaveBeenCalledWith("/?tenant=zztest"));
  });

  it("opens the tenant selector and changes the selected tenant", async () => {
    render(<TenantProvider><TenantSelector /></TenantProvider>);
    fireEvent.click(screen.getByRole("button", { name: /Tenant: ai8/ }));
    fireEvent.click(screen.getByRole("button", { name: "zztest" }));
    await waitFor(() => expect(replace).toHaveBeenCalledWith("/?tenant=zztest"));
  });

  it("renders the conversation empty selection state", () => {
    render(<TenantProvider><ConversationPanel activity={[]} selectedThreadId={null} onSelectThread={vi.fn()} /></TenantProvider>);
    expect(screen.getByText("Select a conversation")).toBeVisible();
  });

  it("renders conversation data and lets the user send an interactive message", async () => {
    vi.stubGlobal("fetch", vi.fn().mockResolvedValue({ json: async () => ({ messages: [
      { id: "00000000-0000-4000-8000-000000000001", role: "user", content: "question", timestamp: new Date().toISOString() },
      { id: "00000000-0000-4000-8000-000000000002", role: "assistant", content: "answer", timestamp: new Date().toISOString(), provider: "gemini", model: "gemini-2.0", tokens_in: 2, tokens_out: 3, cost_usd: 0.01 },
    ] }) }));
    render(<TenantProvider><ConversationPanel activity={[{ id: "a", thread_id: "thread", tenant: "ai8", user_id: "u", content: "hello", provider: "gemini", model: "gemini", input_tokens: 1, output_tokens: 1, tokens_used: 2, cost_usd: 0, thread_cost_usd: 0, processing_time_ms: 1, status: "success", timestamp: new Date().toISOString() }]} selectedThreadId="thread" onSelectThread={vi.fn()} /></TenantProvider>);
    expect(await screen.findByText("answer")).toBeVisible();
    vi.unstubAllGlobals();
  });

  it("renders rich assistant debug views, markdown, and grounding sources", async () => {
    const messageId = "00000000-0000-4000-8000-000000000099";
    const responsePayload = JSON.stringify({
      candidates: [{ groundingMetadata: { groundingChunks: [
        { web: { uri: "https://example.com/research", title: "Research" } },
        { web: { uri: "javascript:alert(1)", title: "Unsafe source" } },
      ] } }],
    });
    vi.stubGlobal("fetch", vi.fn((url: string) => {
      if (url.startsWith("/api/threads/")) {
        return Promise.resolve({ json: async () => ({ messages: [{
          id: messageId,
          role: "assistant",
          content: "| Name | Value |\n| --- | --- |\n| a | `code` |",
          timestamp: new Date().toISOString(),
          provider: "gemini",
          model: "flash",
          tokens_in: 4,
          tokens_out: 8,
          cost_usd: 0.01,
          grounding_queries: 1,
          grounding_cost_usd: 0.002,
        }] }) });
      }
      return Promise.resolve({ json: async () => ({
        rendered_html: "<p>sanitized elsewhere</p>",
        raw_request_json: '{"prompt":"hello"}',
        raw_response_json: responsePayload,
      }) });
    }));
    render(<TenantProvider><ConversationPanel activity={[{ id: "a", thread_id: "thread", tenant: "ai8", user_id: "u", content: "hello", provider: "gemini", model: "flash", input_tokens: 4, output_tokens: 8, tokens_used: 12, cost_usd: 0.01, grounding_queries: 1, grounding_cost_usd: 0.002, thread_cost_usd: 0.012, processing_time_ms: 1, status: "success", timestamp: new Date().toISOString() }]} selectedThreadId="thread" onSelectThread={vi.fn()} /></TenantProvider>);
    expect(await screen.findByText("Research")).toBeVisible();
    fireEvent.click(screen.getAllByRole("button", { name: "Markdown" })[0]);
    expect(await screen.findByRole("table")).toBeVisible();
    fireEvent.click(screen.getAllByRole("button", { name: "Raw" })[0]);
    expect(screen.getByText(/\| Name \| Value \|/)).toBeVisible();
    fireEvent.click(screen.getAllByRole("button", { name: "Request" })[0]);
    expect(await screen.findByText(/"prompt": "hello"/)).toBeVisible();
    fireEvent.click(screen.getAllByRole("button", { name: "Response" })[0]);
    expect(screen.getByText("Web Sources (2)")).toBeVisible();
    expect(screen.getByText("Unsafe source")).toBeVisible();
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

  it("shows missing debug data and closes on the backdrop", async () => {
    const close = vi.fn();
    vi.stubGlobal("fetch", vi.fn().mockResolvedValue({ ok: false, status: 404, statusText: "Not Found", json: async () => ({}) }));
    const { container } = render(<DebugModal messageId="message" onClose={close} />);
    expect(await screen.findByText("Debug data not found.")).toBeVisible();
    fireEvent.click(container.firstElementChild!);
    expect(close).toHaveBeenCalledOnce();
    vi.unstubAllGlobals();
  });

  it("renders parsed debug metadata, citations, errors, and raw JSON", async () => {
    const close = vi.fn();
    vi.stubGlobal("fetch", vi.fn().mockResolvedValue({ ok: true, json: async () => ({
      tenant_id: "ai8", user_id: "user", request_provider: "gemini", response_model: "flash",
      timestamp: new Date().toISOString(), duration_ms: 1500, tokens_in: 3, tokens_out: 5,
      cost_usd: 0.012, grounding_queries: 2, grounding_cost_usd: 0.002, thread_id: "thread",
      system_prompt: "system", user_input: "input", response_text: "output",
      citations: JSON.stringify([
        { type: "url", uri: "https://example.com/a", title: "Example" },
        { type: "url", uri: "javascript:blocked", title: "Blocked" },
        { type: "file", filename: "notes.txt", snippet: "useful note" },
      ]),
      raw_request_json: '{"request":true}', raw_response_json: "not-json",
    }) }));
    render(<DebugModal messageId="message" onClose={close} />);
    expect(await screen.findByText("Cost Breakdown")).toBeVisible();
    expect(screen.getByText("Web Sources")).toBeVisible();
    expect(screen.getByText("File Sources")).toBeVisible();
    expect(screen.getByText("notes.txt")).toBeVisible();
    fireEvent.click(screen.getByRole("button", { name: "JSON" }));
    expect(await screen.findByText(/"request": true/)).toBeVisible();
    expect(screen.getByText("not-json")).toBeVisible();
    fireEvent.click(screen.getByRole("button", { name: "Parsed" }));
    fireEvent.click(screen.getByRole("button", { name: "Close" }));
    expect(close).toHaveBeenCalledOnce();
    vi.unstubAllGlobals();
  });

  it("sends a new conversation message and renders the assistant reply", async () => {
    const fetch = vi.fn().mockResolvedValue({ json: async () => ({ id: "reply", content: "generated reply", provider: "gemini", model: "m" }) });
    vi.stubGlobal("fetch", fetch);
    render(<TenantProvider><ConversationPanel activity={[]} selectedThreadId={null} onSelectThread={vi.fn()} /></TenantProvider>);
    const input = screen.getByPlaceholderText("Start a new conversation...");
    fireEvent.change(input, { target: { value: "draft message" } });
    fireEvent.keyDown(input, { key: "Enter" });
    expect(await screen.findByText("generated reply")).toBeVisible();
    expect(fetch).toHaveBeenCalledWith("/api/chat", expect.objectContaining({ method: "POST" }));
    vi.unstubAllGlobals();
  });

  it("creates a thread and configures a custom system prompt", async () => {
    const select = vi.fn();
    render(<TenantProvider><ConversationPanel activity={[]} selectedThreadId={null} onSelectThread={select} /></TenantProvider>);
    fireEvent.click(screen.getByTitle("Start new conversation"));
    expect(select).toHaveBeenCalledWith(expect.stringMatching(/^[0-9a-f-]{36}$/));
    fireEvent.click(screen.getByRole("button", { name: /Email4.ai/ }));
    fireEvent.click(screen.getByText("Custom", { selector: "span" }));
    fireEvent.change(screen.getByPlaceholderText(/custom system prompt/i), { target: { value: "custom" } });
    expect(screen.getByDisplayValue("custom")).toBeVisible();
  });
});
