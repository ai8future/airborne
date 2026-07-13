import { expect, test } from "@playwright/test";

test("dashboard renders the activity empty state", async ({ page }) => {
  await page.route("**/api/activity?**", async route => {
    await route.fulfill({ contentType: "application/json", body: JSON.stringify({ activity: [] }) });
  });

  await page.goto("/");
  await expect(page.getByRole("heading", { name: "Live Activity Feed" })).toBeVisible();
  await expect(page.getByText("No recent activity. Requests will appear here as they are processed.")).toBeVisible();
});

test("dashboard exposes API failure state", async ({ page }) => {
  await page.route("**/api/activity?**", async route => {
    await route.fulfill({ contentType: "application/json", body: JSON.stringify({ activity: [], error: "backend unavailable" }) });
  });

  await page.goto("/");
  await expect(page.getByText("backend unavailable")).toBeVisible();
});
