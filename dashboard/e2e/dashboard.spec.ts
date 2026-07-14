import { expect, test } from "@playwright/test";

const liveStack = process.env.DASHBOARD_E2E_LIVE === "1";

test.describe("standalone mocked dashboard", () => {
  test.skip(liveStack, "mocked smoke checks are separate from the live-stack contract");

  test("renders the activity empty state", async ({ page }) => {
    await page.route("**/api/activity?**", async route => {
      await route.fulfill({ contentType: "application/json", body: JSON.stringify({ activity: [] }) });
    });

    await page.goto("/");
    await expect(page.getByRole("heading", { name: "Live Activity Feed" })).toBeVisible();
    await expect(
      page.getByText("No recent activity. Requests will appear here as they are processed."),
    ).toBeVisible();
  });

  test("exposes API failure state", async ({ page }) => {
    await page.route("**/api/activity?**", async route => {
      await route.fulfill({
        contentType: "application/json",
        body: JSON.stringify({ activity: [], error: "backend unavailable" }),
      });
    });

    await page.goto("/");
    await expect(page.getByText("backend unavailable")).toBeVisible();
  });

  test("sends a new conversation through the browser without a backend", async ({ page }) => {
    await page.route("**/api/activity?**", async route => {
      await route.fulfill({ contentType: "application/json", body: JSON.stringify({ activity: [] }) });
    });
    await page.route("**/api/threads/**", async route => {
      await route.fulfill({ contentType: "application/json", body: JSON.stringify({ messages: [] }) });
    });
    await page.route("**/api/chat", async route => {
      expect(route.request().postDataJSON()).toMatchObject({
        message: "Mocked browser request",
        tenant_id: "ai8",
      });
      await new Promise(resolve => setTimeout(resolve, 50));
      await route.fulfill({
        contentType: "application/json",
        body: JSON.stringify({
          id: "mocked-response",
          content: "Mocked assistant response",
          provider: "stub",
          model: "deterministic",
        }),
      });
    });

    await page.goto("/?tenant=ai8");
    const input = page.getByPlaceholder("Start a new conversation...");
    await input.fill("Mocked browser request");
    const chatResponsePromise = page.waitForResponse(
      response => response.url().includes("/api/chat") && response.request().method() === "POST",
    );
    await input.press("Enter");
    expect((await chatResponsePromise).ok()).toBe(true);
    await expect(page.getByText("Mocked assistant response")).toBeVisible();
  });
});

test.describe("live dashboard stack", () => {
  test.skip(!liveStack, "set DASHBOARD_E2E_LIVE=1 to exercise the OrbStack-backed stack");

  test("proxies authenticated activity and chat through the running stack", async ({ page }) => {
    test.setTimeout(45_000);
    const diagnostics: string[] = [];
    page.on("console", message => diagnostics.push(`console:${message.type()}:${message.text()}`));
    page.on("requestfailed", request => {
      diagnostics.push(`requestfailed:${request.method()}:${request.url()}:${request.failure()?.errorText || "unknown"}`);
    });
    page.on("response", response => {
      if (response.url().includes("/api/")) {
        diagnostics.push(`response:${response.status()}:${response.request().method()}:${response.url()}`);
      }
    });

    const token =
      process.env.DASHBOARD_E2E_TOKEN ||
      process.env.DASHBOARD_ADMIN_TOKEN ||
      process.env.AIRBORNE_ADMIN_TOKEN ||
      "";
    expect(token, "live E2E requires DASHBOARD_E2E_TOKEN or an admin-token environment variable").not.toBe("");

    await page.setExtraHTTPHeaders({ Authorization: `Bearer ${token}` });
    await page.goto("/?tenant=ai8");
    await expect(page.getByRole("heading", { name: "Live Activity Feed" })).toBeVisible();
    await expect(page.getByText("dashboard admin token is not configured")).toHaveCount(0);
    await expect(page.getByText("unauthorized")).toHaveCount(0);

    const activityResponse = await page.request.get("/api/activity?limit=1&tenant_id=ai8", {
      headers: { Authorization: `Bearer ${token}` },
    });
    expect(activityResponse.ok(), await activityResponse.text()).toBe(true);

    await page.getByRole("button", { name: "Start new conversation" }).click();
    const input = page.getByPlaceholder(/Ask anything|Start a new conversation/);
    await expect(input).toHaveValue("");
    await expect(input).toBeEnabled();
    const prompt = `Live stack probe ${Date.now()}`;
    await input.fill(prompt);
    const chatResponsePromise = page.waitForResponse(
      response => response.url().includes("/api/chat") && response.request().method() === "POST",
    );
    const sendButton = input.locator("xpath=following-sibling::button[1]");
    await expect(sendButton).toBeEnabled();
    await sendButton.click();
    const chatResponse = await Promise.race([
      chatResponsePromise,
      page.waitForTimeout(20_000).then(() => {
        throw new Error(`live chat response exceeded 20 seconds\n${diagnostics.join("\n")}`);
      }),
    ]);
    const chatBody = await chatResponse.text();
    expect(chatResponse.ok(), `${chatBody}\n${diagnostics.join("\n")}`).toBe(true);
    await expect(page.getByText(prompt, { exact: true })).toBeVisible();
    await expect(page.getByText("deterministic-e2e-response", { exact: true }).last()).toBeVisible();
  });
});
