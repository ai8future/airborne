import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";

const replace = vi.fn();
vi.mock("next/navigation", () => ({
  useSearchParams: () => new URLSearchParams("tenant=ai8"),
  useRouter: () => ({ replace }),
  usePathname: () => "/",
}));

import ConversationPanel from "@/components/ConversationPanel";
import { TenantProvider } from "@/context/TenantContext";

const persistedMessageId = "11111111-1111-4111-8111-111111111111";
const timestamp = "2026-07-14T09:30:00.000Z";

function activityEntry(overrides: Record<string, unknown> = {}) {
  return {
    id: "activity-1",
    thread_id: "thread-1",
    tenant: "ai8",
    user_id: "operator",
    content: "activity summary",
    full_content: "full activity response",
    provider: "gemini",
    model: "gemini-2.5-pro",
    input_tokens: 30,
    output_tokens: 12,
    tokens_used: 42,
    cost_usd: 0.012,
    grounding_queries: 2,
    grounding_cost_usd: 0.003,
    thread_cost_usd: 0.015,
    processing_time_ms: 500,
    status: "success",
    timestamp,
    ...overrides,
  };
}

function renderPanel(
  props: Partial<React.ComponentProps<typeof ConversationPanel>> = {},
) {
  const onSelectThread = vi.fn<(threadId: string) => void>(
    props.onSelectThread || (() => undefined),
  );
  const view = render(
    <TenantProvider>
      <ConversationPanel
        activity={props.activity || []}
        selectedThreadId={props.selectedThreadId ?? null}
        onSelectThread={onSelectThread}
      />
    </TenantProvider>,
  );
  return { ...view, onSelectThread };
}

describe("conversation panel workflows", () => {
  afterEach(() => {
    vi.unstubAllGlobals();
    vi.restoreAllMocks();
    replace.mockReset();
  });

  it("renders persisted messages across formatted, markdown, raw, request, and response views", async () => {
    const responseJSON = JSON.stringify({
      padding: "x".repeat(50_100),
      candidates: [
        {
          groundingMetadata: {
            groundingChunks: [
              { web: { uri: "https://docs.example.test/path", title: "Docs" } },
              { web: { uri: "javascript:alert(1)", title: "Unsafe source" } },
            ],
          },
        },
      ],
    });
    const assistantContent = [
      "Grounded `inline` answer.",
      "",
      "| Key | Value |",
      "| --- | --- |",
      "| one | two |",
      "",
      "```js",
      "const answer = true;",
      "```",
    ].join("\n");
    const fetchMock = vi.fn(async (input: string | URL | Request, _init?: RequestInit) => {
      const url = String(input);
      if (url === "/api/threads/thread-1") {
        return {
          json: async () => ({
            messages: [
              { id: "user-1", role: "user", content: "Original question", timestamp },
              {
                id: persistedMessageId,
                role: "assistant",
                content: assistantContent,
                timestamp,
                provider: "gemini",
                model: "gemini-2.5-pro",
                tokens_in: 30,
                tokens_out: 12,
                cost_usd: 0.012,
                grounding_queries: 2,
                grounding_cost_usd: 0.003,
              },
            ],
          }),
        };
      }
      if (url === `/api/debug/${persistedMessageId}`) {
        return {
          json: async () => ({
            rendered_html: "<p>stored rendering</p>",
            raw_request_json: JSON.stringify({ prompt: "hello" }),
            raw_response_json: responseJSON,
          }),
        };
      }
      throw new Error(`unexpected request: ${url}`);
    });
    vi.stubGlobal("fetch", fetchMock);

    renderPanel({
      selectedThreadId: "thread-1",
      activity: [
        activityEntry({ timestamp: "2026-07-13T08:00:00.000Z", content: "older" }),
        activityEntry({ id: "activity-2", content: "newer", thread_cost_usd: 0.02 }),
      ],
    });

    expect(await screen.findByText("Original question")).toBeVisible();
    expect(await screen.findByText("Docs")).toBeVisible();
    expect(screen.getByText("Unsafe source")).toBeVisible();
    expect(screen.getByText("2 msgs")).toBeVisible();
    expect(screen.getByText("42")).toBeVisible();
    expect(screen.getByText("2.5-pro")).toBeVisible();

    const markdownButtons = screen.getAllByRole("button", { name: "Markdown" });
    fireEvent.click(markdownButtons.at(-1)!);
    expect(screen.getByRole("table")).toBeVisible();
    expect(screen.getByText("const answer = true;")).toBeVisible();

    fireEvent.click(screen.getAllByRole("button", { name: "Raw" }).at(-1)!);
    expect(screen.getByText(/Grounded `inline` answer/)).toBeVisible();

    fireEvent.click(screen.getAllByRole("button", { name: "Request" }).at(-1)!);
    expect(screen.getByText(/"prompt": "hello"/)).toBeVisible();

    fireEvent.click(screen.getAllByRole("button", { name: "Response" }).at(-1)!);
    expect(screen.getByText(/\[truncated -/)).toBeVisible();
    expect(screen.getByRole("link", { name: "Docs" })).toHaveAttribute(
      "href",
      "https://docs.example.test/path",
    );

    fireEvent.click(screen.getAllByRole("button", { name: "Request" })[0]);
    expect(screen.getByText(/message not persisted yet/)).toBeVisible();
    expect(screen.getByText("No request data available")).toBeVisible();
  });

  it("uploads a file and sends a custom-prompt message deterministically", async () => {
    const fetchMock = vi.fn(async (input: string | URL | Request, _init?: RequestInit) => {
      const url = String(input);
      if (url === "/api/threads/thread-1") {
        return { json: async () => ({ messages: [] }) };
      }
      if (url === "/api/upload") {
        return {
          json: async () => ({
            file_uri: "gs://bucket/brief.txt",
            mime_type: "text/plain",
            filename: "brief.txt",
          }),
        };
      }
      if (url === "/api/chat") {
        return {
          json: async () => ({
            id: "response-not-persisted",
            content: "Assistant reply",
            provider: "gemini",
            model: "gemini-2.5-pro",
            tokens_in: 8,
            tokens_out: 3,
            cost_usd: 0.001,
          }),
        };
      }
      throw new Error(`unexpected request: ${url}`);
    });
    vi.stubGlobal("fetch", fetchMock);
    vi.spyOn(globalThis.crypto, "randomUUID").mockReturnValue(
      "22222222-2222-4222-8222-222222222222",
    );

    const { container } = renderPanel({
      selectedThreadId: "thread-1",
      activity: [activityEntry()],
    });
    expect(await screen.findByText("full activity response")).toBeVisible();

    const file = new File(["facts"], "brief.txt", { type: "text/plain" });
    const fileInput = container.querySelector('input[type="file"]') as HTMLInputElement;
    fireEvent.change(fileInput, { target: { files: [file] } });
    expect(screen.getByTitle("brief.txt")).toBeVisible();

    fireEvent.click(screen.getByRole("button", { name: "Email4.ai" }));
    fireEvent.click(screen.getByRole("button", { name: "Custom" }));
    const promptEditor = screen.getByPlaceholderText("Enter your custom system prompt...");
    fireEvent.change(promptEditor, { target: { value: "Custom operating prompt" } });
    expect(screen.getByText("23 characters")).toBeVisible();
    fireEvent.click(screen.getByRole("button", { name: "Done" }));

    const input = screen.getByPlaceholderText("Ask anything...") as HTMLTextAreaElement;
    Object.defineProperty(input, "scrollHeight", { configurable: true, value: 80 });
    fireEvent.change(input, { target: { value: "Analyze this" } });
    fireEvent.input(input);
    expect(input.style.height).toBe("80px");
    fireEvent.keyDown(input, { key: "Enter", shiftKey: false });

    expect(await screen.findByText("Assistant reply")).toBeVisible();
    expect(screen.getByTitle("Attach file")).toBeVisible();
    expect(fetchMock).toHaveBeenCalledWith(
      "/api/upload",
      expect.objectContaining({ method: "POST", body: expect.any(FormData) }),
    );
    const chatCall = fetchMock.mock.calls.find(([url]) => url === "/api/chat");
    expect(chatCall).toBeDefined();
    const chatBody = JSON.parse(String((chatCall?.[1] as RequestInit).body));
    expect(chatBody).toMatchObject({
      thread_id: "thread-1",
      message: "Analyze this",
      system_prompt: "Custom operating prompt",
      tenant_id: "ai8",
      file_uri: "gs://bucket/brief.txt",
      file_mime_type: "text/plain",
      filename: "brief.txt",
      request_id: "22222222-2222-4222-8222-222222222222",
    });
  });

  it("creates new conversations and removes an optimistic message after an API error", async () => {
    const fetchMock = vi.fn().mockResolvedValue({ json: async () => ({ error: "provider unavailable" }) });
    vi.stubGlobal("fetch", fetchMock);
    vi.stubGlobal("crypto", null);
    vi.spyOn(Math, "random").mockReturnValue(0);
    const consoleError = vi.spyOn(console, "error").mockImplementation(() => undefined);
    const { onSelectThread } = renderPanel();

    fireEvent.click(screen.getByTitle("Start new conversation"));
    const input = screen.getByPlaceholderText("Start a new conversation...");
    fireEvent.change(input, { target: { value: "Message that fails" } });
    fireEvent.keyDown(input, { key: "Enter", shiftKey: false });

    await waitFor(() => expect(fetch).toHaveBeenCalledWith("/api/chat", expect.any(Object)));
    await waitFor(() => expect(screen.queryByText("Message that fails")).not.toBeInTheDocument());
    const chatCall = fetchMock.mock.calls.find(([url]) => url === "/api/chat");
    expect(JSON.parse(String((chatCall?.[1] as RequestInit).body)).request_id).toMatch(
      /^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/,
    );
    expect(onSelectThread).toHaveBeenCalledTimes(2);
    const selectedThreadIds = onSelectThread.mock.calls.map(([threadId]) => threadId);
    expect(selectedThreadIds).toEqual([
      expect.stringMatching(/^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/),
      expect.stringMatching(/^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/),
    ]);
    expect(new Set(selectedThreadIds).size).toBe(2);
    consoleError.mockRestore();
  });

  it("stops before chat when an attachment upload fails", async () => {
    const fetchMock = vi.fn(async (input: string | URL | Request) => {
      const url = String(input);
      if (url === "/api/threads/thread-1") return { json: async () => ({ messages: [] }) };
      if (url === "/api/upload") return { json: async () => ({ error: "upload rejected" }) };
      throw new Error(`unexpected request: ${url}`);
    });
    vi.stubGlobal("fetch", fetchMock);
    const consoleError = vi.spyOn(console, "error").mockImplementation(() => undefined);
    const { container } = renderPanel({ selectedThreadId: "thread-1" });
    await waitFor(() => expect(fetchMock).toHaveBeenCalledWith("/api/threads/thread-1"));

    const file = new File(["facts"], "brief.txt", { type: "text/plain" });
    fireEvent.change(container.querySelector('input[type="file"]')!, {
      target: { files: [file] },
    });
    const input = screen.getByPlaceholderText("Ask anything...");
    fireEvent.change(input, { target: { value: "Analyze" } });
    fireEvent.keyDown(input, { key: "Enter", shiftKey: false });

    await waitFor(() => expect(fetchMock).toHaveBeenCalledWith("/api/upload", expect.any(Object)));
    await waitFor(() => expect(screen.queryByText(/Analyze/)).not.toBeInTheDocument());
    expect(fetchMock.mock.calls.some(([url]) => url === "/api/chat")).toBe(false);
    consoleError.mockRestore();
  });

  it("recovers from thread fetch failures and auto-selects the newest thread", async () => {
    const consoleError = vi.spyOn(console, "error").mockImplementation(() => undefined);
    vi.stubGlobal("fetch", vi.fn().mockRejectedValue(new Error("offline")));
    const { onSelectThread, rerender } = renderPanel({
      activity: [activityEntry({ thread_id: "thread-new" })],
    });

    await waitFor(() => expect(onSelectThread).toHaveBeenCalledWith("thread-new"));
    rerender(
      <TenantProvider>
        <ConversationPanel
          activity={[activityEntry({ thread_id: "thread-new" })]}
          selectedThreadId="thread-new"
          onSelectThread={onSelectThread}
        />
      </TenantProvider>,
    );
    await waitFor(() => expect(screen.getByText("No messages in this thread")).toBeVisible());
    consoleError.mockRestore();
  });
});
