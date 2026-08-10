import { describe, expect, it, vi, afterEach } from "vitest";
import { NextRequest } from "next/server";

describe("dashboard admin authentication", () => {
  const original = process.env;
  afterEach(() => { process.env = original; vi.resetModules(); });

  it("requires a configured credential and accepts bearer, API-key, and cookie credentials", async () => {
    process.env = { ...original, DASHBOARD_ADMIN_TOKEN: "dashboard-secret" };
    const { requireDashboardAdmin } = await import("@/lib/adminAuth");
    expect(requireDashboardAdmin(new NextRequest("http://dashboard.test/api/activity"))?.status).toBe(401);
    expect(requireDashboardAdmin(new NextRequest("http://dashboard.test/api/activity", { headers: { authorization: "Bearer dashboard-secret" } }))).toBeNull();
    expect(requireDashboardAdmin(new NextRequest("http://dashboard.test/api/activity", { headers: { "x-api-key": "dashboard-secret" } }))).toBeNull();
    expect(requireDashboardAdmin(new NextRequest("http://dashboard.test/api/activity", { headers: { cookie: "airborne_admin_token=dashboard-secret" } }))).toBeNull();
  });

  it("rejects missing configuration and cross-origin cookie mutations", async () => {
    process.env = { ...original, DASHBOARD_ADMIN_TOKEN: "" };
    let auth = await import("@/lib/adminAuth");
    expect(auth.requireDashboardAdmin(new NextRequest("http://dashboard.test/api/activity"))?.status).toBe(503);
    vi.resetModules();
    process.env = { ...original, DASHBOARD_ADMIN_TOKEN: "secret" };
    auth = await import("@/lib/adminAuth");
    expect(auth.requireDashboardAdmin(new NextRequest("http://dashboard.test/api/chat", { method: "POST", headers: { cookie: "airborne_admin_token=secret", origin: "https://evil.test" } }))?.status).toBe(403);
    expect(auth.requireDashboardAdmin(new NextRequest("http://dashboard.test/api/chat", { method: "POST", headers: { cookie: "airborne_admin_token=secret", origin: "http://dashboard.test" } }))).toBeNull();
  });

  it("forwards the configured backend bearer token without overwriting headers", async () => {
    process.env = { ...original, AIRBORNE_ADMIN_TOKEN: "backend-secret" };
    const { adminFetchHeaders } = await import("@/lib/adminAuth");
    expect(adminFetchHeaders({ "Content-Type": "application/json" })).toEqual({ "Content-Type": "application/json", Authorization: "Bearer backend-secret" });
  });
});
