import { expect, test, type Page } from "@playwright/test";
import { DEMO_STATE } from "./accounts";
import { gotoReady } from "./helpers";

test.use({ storageState: DEMO_STATE });

async function openFromCatalog(page: Page, query: string) {
  await gotoReady(page, `/catalog?q=${encodeURIComponent(query)}`);
  await page.getByTestId("book-card").first().click();
  await expect(page).toHaveURL(/\/books\/[0-9a-f-]{36}$/);
  await page.locator("html[data-hydrated='true']").waitFor({ state: "attached" });
}

test.describe("/books/[id] — screen 3 (docs/05 §5, docs/07 §7)", () => {
  test("Dune detail: banner, wayfinding ladder, record, manifest and neighbors", async ({
    page,
  }) => {
    await openFromCatalog(page, "dune");

    await expect(page.getByTestId("book-title")).toHaveText("Dune");
    await expect(page.getByTestId("book-section")).toContainText("Science Fiction & Speculative");

    const banner = page.getByTestId("availability-banner");
    await expect(banner).toHaveAttribute("data-state", "available");
    await expect(banner).toContainText("Available in Building — 2 copies ready for pickup");

    // Wayfinding ladder (docs/05 §3): path token + four steps.
    await expect(page.getByTestId("wayfinding-path")).toHaveText("MAIN/3F/SF/A-1/S-2");
    const steps = page.getByTestId("ladder-steps").getByRole("listitem");
    await expect(steps).toHaveText([
      "1Floor 3 — North Stacks",
      "2Section: Science Fiction & Speculative",
      "3Aisle A-1",
      "4Shelf S-2 — Row 4, Eye Level",
    ]);

    // Catalog record from the seed.
    const record = page.getByTestId("catalog-record");
    await expect(record).toContainText("9780441013593");
    await expect(record).toContainText("813.54");
    await expect(record).toContainText("PS3558.E63 D8 1965");
    await expect(record).toContainText("Ace Books");

    // Copy manifest (docs/13 §5): C01 checked out on S-2, C02 available, C03 on S-3.
    const rows = page.getByTestId("copy-row");
    await expect(rows).toHaveCount(3);
    await expect(rows.nth(0)).toContainText("ATH-002941-C01");
    await expect(rows.nth(0).getByTestId("copy-status")).toHaveText("Checked Out");
    await expect(rows.nth(1).getByTestId("copy-status")).toHaveText("Available");
    await expect(rows.nth(2)).toContainText("MAIN/3F/SF/A-1/S-3");
    await expect(rows.nth(2).getByTestId("copy-shelf-line")).toHaveText("Floor 3 • Shelf S-3");

    // Affinity carousel + Ask-AI deep link.
    await expect(
      page.getByTestId("neighbor-link").filter({ hasText: "Neuromancer" }),
    ).toBeVisible();
    await expect(page.getByTestId("ask-ai")).toHaveAttribute("href", "/chat?prefill=Dune");
  });

  test("zero-copy book shows the not-in-building banner and empty manifest", async ({ page }) => {
    await openFromCatalog(page, "Oxford Guide to Library Research");
    await expect(page.getByTestId("book-title")).toHaveText("The Oxford Guide to Library Research");
    await expect(page.getByTestId("availability-banner")).toHaveAttribute(
      "data-state",
      "not_in_building",
    );
    await expect(page.getByTestId("availability-banner")).toContainText(
      "Not in this building's collection",
    );
    await expect(page.getByTestId("copy-manifest")).toContainText(
      "No physical copies are cataloged",
    );
    await expect(page.getByTestId("book-location-card")).toContainText(
      "No shelf location is recorded",
    );
  });

  test("a neighbor link opens another book detail page", async ({ page }) => {
    await openFromCatalog(page, "dune");
    const first = page.getByTestId("neighbor-link").first();
    const name = (await first.innerText()).split("\n")[0];
    await first.click();
    await expect(page.getByTestId("book-title")).toHaveText(name);
  });

  test("unknown id renders the not-in-catalog page with a 404", async ({ page }) => {
    const res = await page.goto("/books/00000000-0000-0000-0000-000000000000");
    expect(res?.status()).toBe(404);
    await expect(page.getByTestId("book-not-found")).toContainText(
      "Not in this building's catalog",
    );
    await page.getByRole("link", { name: "Back to the catalog" }).click();
    await expect(page).toHaveURL(/\/catalog$/);
  });

  test("banner and manifest labels are localized in zh-CN", async ({ page }) => {
    await openFromCatalog(page, "dune");
    await page.getByTestId("lang-zh-CN").click();
    await expect(page.getByTestId("availability-banner")).toContainText(
      "馆内可借 — 2 册可立即取书",
    );
    await expect(page.getByTestId("copy-row").first().getByTestId("copy-status")).toHaveText(
      "已借出",
    );
    await expect(page.getByTestId("wayfinding-path")).toHaveText("MAIN/3F/SF/A-1/S-2"); // machine strings stay
    // Wayfinding is composed from structured parts in Chinese; codes stay as stored (docs/14 §6.1).
    await expect(page.getByTestId("ladder-steps").getByRole("listitem")).toHaveText([
      "13 楼 — 北区书库",
      "2分区：科幻与奇幻",
      "3A-1 通道",
      "4书架 S-2 — 第 4 层，平视高度",
    ]);
    await expect(page.getByTestId("book-section")).toHaveText("科幻与奇幻");
    const firstCopy = page.getByTestId("copy-row").first();
    await expect(firstCopy.getByTestId("copy-shelf-line")).toHaveText("3 楼 • 书架 S-2");
    await expect(firstCopy.getByTestId("copy-path")).toHaveText("MAIN/3F/SF/A-1/S-2");
    for (const leak of ["North Stacks", "Eye Level", "Science Fiction & Speculative"]) {
      await expect(page.getByTestId("book-detail").getByText(leak)).toHaveCount(0);
    }
    await page.getByTestId("lang-en").click();
    await expect(page.getByTestId("availability-banner")).toContainText("Available in Building");
  });
});
