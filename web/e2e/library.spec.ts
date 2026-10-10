import { expect, test } from "@playwright/test";
import { DEMO_STATE } from "./accounts";
import { gotoReady } from "./helpers";

test.use({ storageState: DEMO_STATE });

test.describe("/library — screen 4 (docs/13 §6)", () => {
  test("KPI cards match the seeded demo patron (24 / 2 / 15 / 7)", async ({ page }) => {
    await gotoReady(page, "/library");
    const val = (k: string) => page.getByTestId(`kpi-${k}`).getByTestId("kpi-value");
    await expect(val("total")).toHaveText("24");
    await expect(val("reading")).toHaveText("2");
    await expect(val("read")).toHaveText("15");
    await expect(val("favorites")).toHaveText("7");
    await expect(page.getByTestId("library-row")).toHaveCount(24);
  });

  test("stage tabs filter rows and rows show shelf coordinates and progress", async ({ page }) => {
    await gotoReady(page, "/library");
    await page.getByTestId("tab-reading").click();
    await expect(page).toHaveURL(/stage=reading/);
    const rows = page.getByTestId("library-row");
    await expect(rows).toHaveCount(2);
    const dune = rows.filter({ has: page.locator("[data-testid=row-stage]") }).first();
    await expect(page.locator("[data-title='Dune']")).toContainText("Floor 3 • Shelf S-2");
    await expect(page.locator("[data-title='Dune']").getByTestId("row-progress")).toHaveText(
      "40% read",
    );
    await expect(dune).toBeVisible();

    await page.getByTestId("tab-favorites").click();
    await expect(page.getByTestId("library-row")).toHaveCount(7);
  });

  test("holds tab lists the two seeded waitlist positions and the AI banner deep-links", async ({
    page,
  }) => {
    await gotoReady(page, "/library?stage=holds");
    await expect(page.getByTestId("hold-row")).toHaveCount(2);
    await expect(page.getByTestId("hold-position").first()).toHaveText("Position 1 in queue");
    await expect(page.getByTestId("ai-banner-link")).toHaveAttribute("href", /^\/chat\?prefill=/);
  });

  test("zh-CN localizes the KPI labels and stage names", async ({ page }) => {
    await gotoReady(page, "/library");
    await page.getByTestId("lang-zh-CN").click();
    await expect(page.getByTestId("kpi-total")).toContainText("馆藏总数");
    await page.getByTestId("tab-reading").click();
    await expect(page.getByTestId("row-stage").first()).toHaveText("在读");
    await page.getByTestId("lang-en").click();
  });
});
