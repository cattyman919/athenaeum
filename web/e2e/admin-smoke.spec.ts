import { expect, test, type Page } from "@playwright/test";
import { ADMIN, ADMIN_STATE, DEMO } from "./accounts";
import { gotoReady } from "./helpers";
import { resetRateLimits } from "./rate-limit";

// Admin portal smoke (docs/07 screens 6a/6b, docs/03 §8). KPI numbers are the seeded
// fixture totals of docs/13 §1 (39 titles, 58 copies: 47 available / 11 checked out /
// 0 missing; 40 shelves × 15 slots). Every mutation here is reverted before the test ends
// so the other specs keep seeing the seed counts.
const S2 = "MAIN/3F/SF/A-1/S-2"; // Dune C1/C2 + Neuromancer C1/C2 (docs/13 §5)
const S3 = "MAIN/3F/SF/A-1/S-3"; // Dune C3 home
const DUNE_C2 = "ATH-002941-C02";
const DUNE_C3 = "ATH-002941-C03";

interface TreeShelf {
  id: string;
  path: string;
}

async function shelfIds(page: Page): Promise<Record<string, string>> {
  const res = await page.request.get("/api/admin/tree");
  expect(res.ok()).toBe(true);
  const tree = (await res.json()) as {
    floors: { sections: { aisles: { shelves: TreeShelf[] }[] }[] }[];
  };
  const out: Record<string, string> = {};
  for (const f of tree.floors)
    for (const s of f.sections)
      for (const a of s.aisles) for (const sh of a.shelves) out[sh.path] = sh.id;
  return out;
}

async function openShelf(page: Page, path: string) {
  const [, floor, section, aisle] = path.split("/");
  const floorNode = page.locator(`[data-testid=tree-floor][data-code="${floor.replace("F", "")}"]`);
  const sectionNode = floorNode.locator(`[data-testid=tree-section][data-code="${section}"]`);
  const aisleNode = sectionNode.locator(`[data-testid=tree-aisle][data-code="${aisle}"]`);
  if ((await floorNode.getAttribute("aria-expanded")) !== "true")
    await floorNode.locator("> button").click();
  if ((await sectionNode.getAttribute("aria-expanded")) !== "true")
    await sectionNode.locator("> button").click();
  if ((await aisleNode.getAttribute("aria-expanded")) !== "true")
    await aisleNode.locator("> button").click();
  await page.locator(`[data-testid=tree-shelf][data-path="${path}"]`).click();
  await expect(page.getByTestId("inspector-path")).toHaveText(path);
}

test.describe("admin portal smoke", () => {
  test.use({ storageState: ADMIN_STATE });
  test.beforeAll(async () => {
    await resetRateLimits();
  });

  test("KPI grid matches the seeded fixture totals (docs/13 §1)", async ({ page }) => {
    await gotoReady(page, "/admin");
    const kpi = (key: string) => page.locator(`[data-testid=kpi-card][data-kpi="${key}"]`);
    await expect(kpi("titles").getByTestId("kpi-value")).toHaveText("39");
    await expect(kpi("copies").getByTestId("kpi-value")).toHaveText("58");
    await expect(page.getByTestId("kpi-copies-available")).toHaveText("47 available");
    await expect(page.getByTestId("kpi-copies-checked-out")).toHaveText("11 checked out");
    await expect(page.getByTestId("kpi-copies-missing")).toHaveText("0 missing");
    await expect(page.getByTestId("kpi-capacity-slots")).toHaveText("58 / 600 slots occupied");
    await expect(kpi("capacity").getByTestId("kpi-value")).toHaveText("10%");
    await expect(kpi("patrons").getByTestId("kpi-value")).toHaveText(/^\d+$/);
    await expect(page.getByTestId("audit-stream")).toBeVisible();
    await expect(page.getByTestId("add-book-open")).toBeVisible();
  });

  test("accounts table lists both demo users and guards self-demotion", async ({ page }) => {
    await gotoReady(page, "/admin");
    const table = page.getByTestId("users-table");
    const adminRow = table.locator(`[data-testid=user-row][data-email="${ADMIN.email}"]`);
    const demoRow = table.locator(`[data-testid=user-row][data-email="${DEMO.email}"]`);
    await expect(adminRow.getByTestId("user-role")).toHaveValue("admin");
    await expect(adminRow.getByTestId("user-role")).toBeDisabled();
    await expect(demoRow.getByTestId("user-role")).toHaveValue("user");
    await expect(demoRow.getByTestId("user-role")).toBeEnabled();
  });

  test("tree browse opens the shelf inspector and mini-map, state in the URL", async ({ page }) => {
    await gotoReady(page, "/admin/layout");
    await expect(page.getByTestId("shelf-inspector")).toHaveAttribute("data-state", "empty");
    await openShelf(page, S2);

    const inspector = page.getByTestId("shelf-inspector");
    await expect(inspector).toContainText("Science Fiction");
    await expect(inspector).toContainText("Row 4, Eye Level");
    await expect(
      inspector.locator(`[data-testid=inspector-copy][data-barcode="${DUNE_C2}"]`),
    ).toContainText("Dune");
    await expect(inspector.locator('[data-barcode="ATH-002941-C01"]')).toHaveAttribute(
      "data-status",
      "checked_out",
    );
    await expect(page.getByTestId("floor-minimap")).toHaveAttribute("data-floor", "3");
    await expect(page.locator("[data-testid=minimap-aisle][data-active=true]")).toHaveCount(1);
    await expect(page.locator("[data-testid=minimap-shelf][data-selected=true]")).toHaveCount(1);
    await expect(page).toHaveURL(/\/admin\/layout\?shelf=[0-9a-f-]{36}$/);

    // Reload restores the same manifest from the URL.
    await gotoReady(page, page.url());
    await expect(page.getByTestId("inspector-path")).toHaveText(S2);
    await expect(page.locator(`[data-testid=tree-shelf][data-path="${S2}"]`)).toHaveAttribute(
      "aria-current",
      "true",
    );

    // Adjacent shelf navigation.
    await page.getByTestId("adjacent-shelf").filter({ hasText: "S-3" }).click();
    await expect(page.getByTestId("inspector-path")).toHaveText(S3);
  });

  test("status change from the inspector (reverted)", async ({ page }) => {
    await gotoReady(page, "/admin/layout");
    await openShelf(page, S2);
    const row = page.locator(`[data-testid=inspector-copy][data-barcode="${DUNE_C2}"]`);
    await expect(row).toHaveAttribute("data-status", "available");
    try {
      const patched = page.waitForResponse(
        (r) => r.url().includes("/api/admin/copies/") && r.request().method() === "PATCH",
      );
      await row.getByTestId("copy-status-select").selectOption("missing_lost");
      expect((await patched).status()).toBe(200);
      await expect(row).toHaveAttribute("data-status", "missing_lost");
      await expect(page.getByText(`${DUNE_C2} → Missing / lost.`)).toBeVisible();

      await row.getByTestId("copy-status-select").selectOption("available");
      await expect(row).toHaveAttribute("data-status", "available");
    } finally {
      const res = await page.request.patch(`/api/admin/copies/${DUNE_C2}`, {
        data: { status: "available" },
      });
      expect(res.status()).toBe(200);
    }
  });

  test("quick-assign re-shelves a copy by barcode (reverted)", async ({ page }) => {
    const ids = await shelfIds(page);
    await gotoReady(page, `/admin/layout?shelf=${ids[S2]}`);
    await expect(page.getByTestId("inspector-path")).toHaveText(S2);
    const occupancy = page.getByTestId("inspector-occupancy");
    const before = Number((await occupancy.textContent())!.split("/")[0]);
    try {
      // Unknown barcode → inline 404 message, no change.
      await page.getByTestId("quick-assign-input").fill("ATH-999999-C09");
      await page.getByTestId("quick-assign-submit").click();
      await expect(page.getByTestId("quick-assign-error")).toHaveText(
        "No copy with barcode ATH-999999-C09.",
      );

      await page.getByTestId("quick-assign-input").fill(DUNE_C3.toLowerCase());
      await page.getByTestId("quick-assign-submit").click();
      await expect(
        page.locator(`[data-testid=inspector-copy][data-barcode="${DUNE_C3}"]`),
      ).toBeVisible();
      await expect(occupancy).toHaveText(`${before + 1}/15`);
      await expect(page.locator(`[data-testid=tree-shelf][data-path="${S2}"]`)).toContainText(
        `${before + 1}/15`,
      );

      // Move it home through the UI: adjacent S-3, quick-assign back.
      await page.getByTestId("adjacent-shelf").filter({ hasText: "S-3" }).click();
      await expect(page.getByTestId("inspector-path")).toHaveText(S3);
      await page.getByTestId("quick-assign-input").fill(DUNE_C3);
      await page.getByTestId("quick-assign-submit").click();
      await expect(
        page.locator(`[data-testid=inspector-copy][data-barcode="${DUNE_C3}"]`),
      ).toBeVisible();
      await expect(page.locator(`[data-testid=tree-shelf][data-path="${S2}"]`)).toContainText(
        `${before}/15`,
      );
    } finally {
      const res = await page.request.patch(`/api/admin/copies/${DUNE_C3}`, {
        data: { shelf_id: ids[S3] },
      });
      expect(res.status()).toBe(200);
    }
  });
});
