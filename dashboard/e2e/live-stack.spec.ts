import { expect, test } from "@playwright/test";

test.describe("live deterministic stack", () => {
  test.skip(process.env.PLAYWRIGHT_LIVE_STACK !== "1", "requires the isolated production E2E stack");

  test("loads the dashboard and invokes its authenticated provider proxy", async ({ page, baseURL }) => {
    if (!baseURL) throw new Error("Playwright baseURL is required for live-stack E2E");

    await page.context().addCookies([{ name: "airborne_admin_token", value: "dashboard-e2e-token", url: baseURL }]);
    await page.goto("/");
    await expect(page.getByRole("heading", { name: "Live Activity Feed" })).toBeVisible();

    const result = await page.evaluate(async () => {
      const response = await fetch("/api/test", {
        method: "POST",
        headers: {
          Authorization: "Bearer dashboard-e2e-token",
          "Content-Type": "application/json",
        },
        body: JSON.stringify({ prompt: "deterministic browser e2e", tenant_id: "ai8", provider: "openai" }),
      });
      return { status: response.status, body: await response.json() };
    });

    expect(result.status).toBe(200);
    expect(result.body).toMatchObject({ reply: "deterministic-e2e-response", provider: "openai", model: "e2e-model" });
  });
});
