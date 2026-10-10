import { expect, test } from "@playwright/test";
import { ADMIN_STATE, DEMO_STATE } from "./accounts";
import { gotoReady } from "./helpers";

// Admin role gate (docs/04 §7, docs/07 §4): patrons get the localized FORBIDDEN state on
// every /admin* page, no admin data is rendered, and the nav link is admin-only.
test.describe("admin role gate — patron", () => {
  test.use({ storageState: DEMO_STATE });

  for (const path of [
    "/admin",
    "/admin/layout",
    "/admin/layout?shelf=00000000-0000-0000-0000-000000000000",
  ]) {
    test(`patron is blocked from ${path}`, async ({ page }) => {
      await gotoReady(page, path);
      await expect(page.getByTestId("admin-forbidden")).toBeVisible();
      await expect(page.getByTestId("kpi-grid")).toHaveCount(0);
      await expect(page.getByTestId("building-tree")).toHaveCount(0);
      await expect(page.getByTestId("admin-nav")).toHaveCount(0);
    });
  }

  test("patron has no Admin nav entry and the API refuses admin data", async ({ page }) => {
    await gotoReady(page, "/catalog");
    const nav = page.getByRole("navigation", { name: "Primary" });
    await expect(nav.getByRole("link")).not.toHaveCount(0);
    await expect(nav.locator('a[href="/admin"]')).toHaveCount(0);
    const res = await page.request.get("/api/admin/data");
    expect(res.status()).toBe(403);
  });
});

test.describe("admin role gate — admin", () => {
  test.use({ storageState: ADMIN_STATE });

  test("admin sees the nav entry and reaches both admin pages", async ({ page }) => {
    await gotoReady(page, "/catalog");
    await page.getByRole("navigation", { name: "Primary" }).locator('a[href="/admin"]').click();
    await expect(page).toHaveURL(/\/admin$/);
    await expect(page.getByTestId("admin-page")).toBeVisible();
    await page.getByTestId("admin-nav").getByRole("link", { name: "Stacks manager" }).click();
    await expect(page).toHaveURL(/\/admin\/layout$/);
    await expect(page.getByTestId("building-tree")).toBeVisible();
  });
});
