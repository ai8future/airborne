import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";

import DebugModal from "@/components/DebugModal";

const completeDebugData = {
  message_id: "message-1",
  thread_id: "thread-1",
  tenant_id: "ai8",
  user_id: "operator",
  timestamp: "2026-07-14T09:30:00.000Z",
  system_prompt: "Be precise",
  user_input: "Summarize the attachment",
  request_model: "gemini-2.5-pro",
  request_provider: "gemini",
  request_timestamp: "2026-07-14T09:30:00.000Z",
  response_text: "A grounded answer",
  response_model: "gemini-2.5-pro",
  tokens_in: 120,
  tokens_out: 45,
  cost_usd: 0.015,
  grounding_queries: 2,
  grounding_cost_usd: 0.005,
  duration_ms: 2450,
  response_id: "response-1",
  citations: JSON.stringify([
    { type: "url", uri: "https://docs.example.test/guide", title: "Remote guide" },
    { type: "url", uri: "javascript:alert(1)", title: "Unsafe guide" },
    { type: "file", filename: "brief.pdf", snippet: "Material fact" },
  ]),
  raw_request_json: JSON.stringify({ request: "value" }),
  raw_response_json: "not-json-response",
  status: "success",
};

describe("debug inspector modal", () => {
  afterEach(() => {
    vi.unstubAllGlobals();
    vi.restoreAllMocks();
  });

  it("renders complete request data, safe citations, raw JSON, and close paths", async () => {
    const onClose = vi.fn();
    vi.stubGlobal(
      "fetch",
      vi.fn().mockResolvedValue({ ok: true, json: async () => completeDebugData }),
    );
    const { container } = render(<DebugModal messageId="message-1" onClose={onClose} />);

    expect(screen.getByText("Loading debug data...")).toBeVisible();
    expect(await screen.findByText("Be precise")).toBeVisible();
    expect(screen.getByText("A grounded answer")).toBeVisible();
    expect(screen.getByText("2 queries")).toBeVisible();
    expect(screen.getByText("brief.pdf")).toBeVisible();
    expect(screen.getByText("Material fact")).toBeVisible();
    expect(screen.getByRole("link", { name: "Remote guide" })).toHaveAttribute(
      "href",
      "https://docs.example.test/guide",
    );
    expect(screen.getByText("Unsafe guide")).toBeVisible();

    fireEvent.click(screen.getByRole("button", { name: "JSON" }));
    expect(screen.getByText(/"request": "value"/)).toBeVisible();
    expect(screen.getByText("not-json-response")).toBeVisible();
    fireEvent.click(screen.getByRole("button", { name: "Parsed" }));

    fireEvent.keyDown(window, { key: "Escape" });
    const backdrop = container.querySelector(".fixed.inset-0");
    expect(backdrop).not.toBeNull();
    fireEvent.click(backdrop!);
    fireEvent.click(screen.getByRole("button", { name: "Close" }));
    expect(onClose).toHaveBeenCalledTimes(3);
  });

  it.each([
    [
      "missing records",
      { ok: false, status: 404, statusText: "Not Found", json: async () => ({}) },
      "Debug data not found.",
    ],
    [
      "server failures",
      { ok: false, status: 500, statusText: "Bad Gateway", json: async () => ({}) },
      "Failed to fetch debug data: Bad Gateway",
    ],
    [
      "API errors",
      { ok: true, status: 200, statusText: "OK", json: async () => ({ error: "access denied" }) },
      "access denied",
    ],
  ])("shows %s", async (_label, response, expected) => {
    vi.stubGlobal("fetch", vi.fn().mockResolvedValue(response));
    render(<DebugModal messageId="message-1" onClose={vi.fn()} />);
    expect(await screen.findByText(expected)).toBeVisible();
  });

  it("reports network failures with a stable fallback", async () => {
    vi.stubGlobal("fetch", vi.fn().mockRejectedValue("offline"));
    render(<DebugModal messageId="message-1" onClose={vi.fn()} />);
    expect(await screen.findByText("Error loading debug data: Unknown error")).toBeVisible();
  });

  it("reports malformed empty API data through the error state", async () => {
    vi.stubGlobal("fetch", vi.fn().mockResolvedValue({ ok: true, json: async () => null }));
    render(<DebugModal messageId="message-1" onClose={vi.fn()} />);
    expect(
      await screen.findByText("Error loading debug data: Cannot read properties of null (reading 'error')"),
    ).toBeVisible();
    await waitFor(() => expect(screen.queryByRole("button", { name: "JSON" })).not.toBeInTheDocument());
  });
});
