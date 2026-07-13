import { afterEach, describe, expect, it, vi } from "vitest";
import { NextRequest } from "next/server";

const originalEnv = process.env;
const token = "dashboard-token";

function request(path: string, init: RequestInit = {}) {
  return new NextRequest(`http://dashboard.test${path}`, {
    ...init,
    headers: { authorization: `Bearer ${token}`, ...(init.headers || {}) },
  });
}

function jsonResponse(body: unknown, status = 200) {
  return new Response(JSON.stringify(body), { status, headers: { "Content-Type": "application/json" } });
}

describe("dashboard proxy routes", () => {
  afterEach(() => {
    process.env = originalEnv;
    vi.unstubAllGlobals();
    vi.resetModules();
  });

  async function load<T>(path: string): Promise<T> {
    process.env = { ...originalEnv, DASHBOARD_ADMIN_TOKEN: token, AIRBORNE_ADMIN_TOKEN: "backend-token", AIRBORNE_ADMIN_URL: "http://admin.test" };
    return import(path) as Promise<T>;
  }

  it("blocks unauthenticated requests before reaching the activity backend", async () => {
    const fetch = vi.fn(); vi.stubGlobal("fetch", fetch);
    const { GET } = await load<typeof import("@/app/api/activity/route")>("@/app/api/activity/route");
    const response = await GET(new NextRequest("http://dashboard.test/api/activity"));
    expect(response.status).toBe(401);
    expect(fetch).not.toHaveBeenCalled();
  });

  it("forwards activity query bounds and backend authentication", async () => {
    const fetch = vi.fn().mockResolvedValue(jsonResponse({ activity: [{ id: "one" }] })); vi.stubGlobal("fetch", fetch);
    const { GET } = await load<typeof import("@/app/api/activity/route")>("@/app/api/activity/route");
    const response = await GET(request("/api/activity?limit=1000&tenant_id=ai8"));
    expect(response.status).toBe(200); expect(await response.json()).toEqual({ activity: [{ id: "one" }] });
    expect(fetch).toHaveBeenCalledWith("http://admin.test/admin/activity?limit=200&tenant_id=ai8", expect.objectContaining({ cache: "no-store", headers: { "Content-Type": "application/json", Authorization: "Bearer backend-token" } }));
  });

  it("normalizes activity backend failures and connection errors", async () => {
    const fetch = vi.fn().mockResolvedValue(jsonResponse({}, 503)); vi.stubGlobal("fetch", fetch);
    const { GET } = await load<typeof import("@/app/api/activity/route")>("@/app/api/activity/route");
    expect(await (await GET(request("/api/activity?limit=nope"))).json()).toEqual({ activity: [], error: "Airborne admin server returned status 503" });
    fetch.mockRejectedValueOnce(new Error("offline"));
    expect(await (await GET(request("/api/activity"))).json()).toEqual({ activity: [], error: "Failed to connect to Airborne admin server: offline" });
  });

  it("forwards chat payloads and falls back to the test route on a chat 404", async () => {
    const fetch = vi.fn().mockResolvedValueOnce(jsonResponse({}, 404)).mockResolvedValueOnce(jsonResponse({ reply: "fallback", provider: "gemini", model: "m", input_tokens: 1, output_tokens: 2 })); vi.stubGlobal("fetch", fetch);
    const { POST } = await load<typeof import("@/app/api/chat/route")>("@/app/api/chat/route");
    const response = await POST(request("/api/chat", { method: "POST", body: JSON.stringify({ thread_id: "thread", message: "hello", tenant_id: "ai8" }) }));
    expect(response.status).toBe(200); expect(await response.json()).toMatchObject({ content: "fallback", provider: "gemini" });
    expect(fetch.mock.calls[0][0]).toBe("http://admin.test/admin/chat");
    expect(fetch.mock.calls[0][1]).toMatchObject({ headers: { "Content-Type": "application/json", Authorization: "Bearer backend-token" } });
    expect(JSON.parse(fetch.mock.calls[0][1].body)).toMatchObject({ thread_id: "thread", message: "hello", tenant_id: "ai8" });
  });

  it("validates chat requests and propagates non-retryable server status", async () => {
    const fetch = vi.fn().mockResolvedValue(jsonResponse({}, 422)); vi.stubGlobal("fetch", fetch);
    const { POST } = await load<typeof import("@/app/api/chat/route")>("@/app/api/chat/route");
    expect((await POST(request("/api/chat", { method: "POST", body: JSON.stringify({ thread_id: "x", message: "" }) }))).status).toBe(400);
    expect((await POST(request("/api/chat", { method: "POST", body: JSON.stringify({ message: "ok" }) }))).status).toBe(400);
    expect((await POST(request("/api/chat", { method: "POST", body: JSON.stringify({ thread_id: "x", message: "ok" }) }))).status).toBe(422);
  });

  it("forwards test requests and reports backend errors", async () => {
    const fetch = vi.fn().mockResolvedValueOnce(jsonResponse({ reply: "yes", provider: "gemini", model: "m", input_tokens: 1, output_tokens: 1, processing_ms: 2 })).mockResolvedValueOnce(jsonResponse({}, 429)); vi.stubGlobal("fetch", fetch);
    const { POST } = await load<typeof import("@/app/api/test/route")>("@/app/api/test/route");
    expect(await (await POST(request("/api/test", { method: "POST", body: JSON.stringify({ prompt: "hello" }) }))).json()).toMatchObject({ reply: "yes" });
    const bad = await POST(request("/api/test", { method: "POST", body: JSON.stringify({ prompt: "hello", provider: "openai" }) }));
    expect(bad.status).toBe(429); expect(await bad.json()).toEqual({ error: "Airborne admin server returned status 429" });
    expect((await POST(request("/api/test", { method: "POST", body: JSON.stringify({ prompt: " " }) }))).status).toBe(400);
  });

  it("handles missing, absent, and failed debug/thread resources", async () => {
    const fetch = vi.fn().mockResolvedValue(jsonResponse({}, 404)); vi.stubGlobal("fetch", fetch);
    const debug = await load<typeof import("@/app/api/debug/[id]/route")>("@/app/api/debug/[id]/route");
    expect((await debug.GET(request("/api/debug/x"), { params: Promise.resolve({ id: "" }) })).status).toBe(400);
    expect((await debug.GET(request("/api/debug/x"), { params: Promise.resolve({ id: "id with spaces" }) })).status).toBe(404);
    const threads = await load<typeof import("@/app/api/threads/[threadId]/route")>("@/app/api/threads/[threadId]/route");
    expect(await (await threads.GET(request("/api/threads/x"), { params: Promise.resolve({ threadId: "missing" }) })).json()).toEqual({ error: "Thread not found", messages: [] });
    fetch.mockRejectedValueOnce(new Error("offline"));
    expect(await (await threads.GET(request("/api/threads/x"), { params: Promise.resolve({ threadId: "x" }) })).json()).toEqual({ error: "Failed to connect to Airborne admin server: offline", messages: [] });
  });

  it("forwards upload form data and rejects absent or oversized files", async () => {
    const fetch = vi.fn().mockResolvedValue(jsonResponse({ file_uri: "file://one", filename: "one.txt" })); vi.stubGlobal("fetch", fetch);
    const { POST } = await load<typeof import("@/app/api/upload/route")>("@/app/api/upload/route");
    const form = new FormData(); form.append("file", new File(["contents"], "one.txt", { type: "text/plain" })); form.append("tenant_id", "ai8");
    expect(await (await POST(request("/api/upload", { method: "POST", body: form }))).json()).toMatchObject({ filename: "one.txt" });
    expect(fetch.mock.calls[0][1]).toMatchObject({ method: "POST", headers: { Authorization: "Bearer backend-token" } });
    expect((await POST(request("/api/upload", { method: "POST", body: new FormData() }))).status).toBe(400);
    expect((await POST(request("/api/upload", { method: "POST", headers: { "content-length": String(101 * 1024 * 1024) }, body: new FormData() }))).status).toBe(413);
  });
});
