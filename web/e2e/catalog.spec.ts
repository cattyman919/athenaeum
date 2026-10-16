import { expect, test, type Page } from "@playwright/test";
import { DEMO_STATE } from "./accounts";
import { gotoReady, switchLang } from "./helpers";
import { resetRateLimits } from "./rate-limit";

test.use({ storageState: DEMO_STATE });

const cards = (page: Page) => page.getByTestId("book-card");
const titles = async (page: Page) =>
  cards(page).evaluateAll((els) => els.map((e) => e.getAttribute("data-title")));

test.beforeAll(async () => {
  await resetRateLimits();
});

test.describe("/catalog — screen 2 (docs/07 §7, docs/13 §5)", () => {
  test("browse shows 12 per page of 40 volumes with pagination", async ({ page }) => {
    await gotoReady(page, "/catalog");
    await expect(page.getByTestId("result-count")).toHaveText("40 volumes");
    await expect(cards(page)).toHaveCount(12);
    await expect(page.getByText("Page 1 of 4")).toBeVisible();

    await page.getByRole("link", { name: "Next" }).click();
    await expect(page).toHaveURL(/page=2/);
    await expect(page.getByText("Page 2 of 4")).toBeVisible();
    await expect(page.getByRole("link", { name: "Previous" })).toBeVisible();
  });

  test("keyword search ranks Dune first and the URL carries the state", async ({ page }) => {
    await gotoReady(page, "/catalog");
    await page.getByTestId("catalog-search-input").fill("dune");
    await page.getByTestId("catalog-search-submit").click();
    await expect(page).toHaveURL(/q=dune/);
    await expect(cards(page).first()).toHaveAttribute("data-title", "Dune");
    const dune = cards(page).first();
    await expect(dune.getByTestId("availability-badge")).toHaveText("Available");
    await expect(dune).toContainText("MAIN/3F/SF/A-1/S-2");
    await expect(dune).toContainText("2 of 3 copies");

    // Shareable: a fresh load of the same URL renders the same results.
    await page.reload();
    await expect(cards(page).first()).toHaveAttribute("data-title", "Dune");
  });

  test("semantic search uses real embeddings (Foundation for a galactic-empire question)", async ({
    page,
  }) => {
    await gotoReady(
      page,
      "/catalog?mode=semantic&q=the+fall+of+a+galactic+empire+and+psychohistory",
    );
    await expect(page.getByTestId("mode-semantic")).toHaveAttribute("data-state", "active");
    await expect(cards(page).first()).toBeVisible(); // Ollama round-trip streams in behind the skeleton
    const top = (await titles(page)).slice(0, 3);
    expect(top).toContain("Foundation");
  });

  test("hybrid search fuses keyword and semantic rankings", async ({ page }) => {
    await gotoReady(page, "/catalog?mode=hybrid&q=dune+desert+planet");
    await expect(cards(page).first()).toHaveAttribute("data-title", "Dune");
  });

  test("facets: section, availability and era filter the grid via the URL", async ({ page }) => {
    await gotoReady(page, "/catalog");
    await page.getByTestId("facet-section-SF").click();
    await expect(page).toHaveURL(/section=SF/);
    await expect(page.getByTestId("result-count")).toHaveText("10 volumes");

    await page.getByTestId("facet-available").click();
    await expect(page).toHaveURL(/availability=available/);
    await page.getByTestId("facet-era-2000_n").click();
    await expect(page.getByTestId("result-count")).toHaveText("2 volumes");
    expect((await titles(page)).sort()).toEqual(["Project Hail Mary", "The Three-Body Problem"]);

    await page.getByTestId("clear-filters").click();
    await expect(page.getByTestId("result-count")).toHaveText("40 volumes");
  });

  test("availability facet matches docs/13 (35 of 40 books have a copy on the shelf)", async ({
    page,
  }) => {
    await gotoReady(page, "/catalog?availability=available");
    await expect(page.getByTestId("result-count")).toHaveText("35 volumes");
  });

  test("zero-copy books show the not-in-building beacon", async ({ page }) => {
    await gotoReady(page, "/catalog?q=Oxford+Guide+to+Library+Research");
    const card = cards(page).first();
    await expect(card).toHaveAttribute("data-title", "The Oxford Guide to Library Research");
    await expect(card.getByTestId("availability-badge")).toHaveText("Not in Building");
    await expect(card).toContainText("No copies");
  });

  test("sorting by year descending puts the newest SF title first", async ({ page }) => {
    await gotoReady(page, "/catalog?section=SF&sort=year:desc");
    await expect(cards(page).first()).toHaveAttribute("data-title", "Project Hail Mary");
  });

  test("empty search shows the pill with nearby sections", async ({ page }) => {
    await gotoReady(page, "/catalog?q=zzzzqqqq");
    const empty = page.getByTestId("catalog-empty");
    await expect(empty).toContainText("No volumes matched");
    await empty.getByRole("link", { name: "Science Fiction & Speculative" }).click();
    await expect(page).toHaveURL(/section=SF/);
    await expect(page.getByTestId("result-count")).toHaveText("10 volumes");
  });

  test("⌘K palette searches the same endpoint and opens a result", async ({ page }) => {
    await gotoReady(page, "/catalog");
    await page.keyboard.press("ControlOrMeta+k");
    await expect(page.getByTestId("command-palette")).toBeVisible();
    await page.getByTestId("palette-input").fill("hyperion");
    await expect(page.getByTestId("palette-item").first()).toContainText("Hyperion");
    // each result carries a jacket thumbnail (img, or the typographic fallback when offline)
    await expect(
      page.getByTestId("palette-item").first().locator("img, [class*=aspect]").first(),
    ).toBeVisible();

    await page.getByTestId("palette-see-all").click();
    await expect(page).toHaveURL(/\/catalog\?q=hyperion/);
    await expect(page.getByTestId("command-palette")).toBeHidden();
  });

  test("semantic/hybrid with an empty query prompts instead of erroring", async ({ page }) => {
    await gotoReady(page, "/catalog?mode=semantic");
    await expect(page.getByTestId("catalog-query-required")).toBeVisible();
    await expect(page.getByTestId("catalog-error")).toHaveCount(0);
    // Switching mode with an empty box stays on the prompt (docs/03 §5.1).
    await page.getByTestId("mode-hybrid").click();
    await expect(page).toHaveURL(/mode=hybrid/);
    await expect(page.getByTestId("catalog-query-required")).toContainText("keyword and meaning");
    // Keyword browsing with an empty query still lists volumes.
    await page.getByTestId("mode-keyword").click();
    await expect(cards(page).first()).toBeVisible();
  });

  test("mode labels and section facets are localized in zh-CN", async ({ page }) => {
    await gotoReady(page, "/catalog");
    await switchLang(page, "zh-CN");
    await expect(page.getByRole("heading", { level: 1 })).toHaveText("馆藏目录");
    await expect(page.getByTestId("mode-semantic")).toHaveText("语义");
    await expect(page.getByTestId("result-count")).toHaveText("共 40 册");
    // Section names come from wayfinding.section.<code> (docs/14 §6.1).
    await expect(page.getByTestId("facet-sidebar")).toContainText("科幻与奇幻");
    await expect(page.getByTestId("facet-sidebar")).not.toContainText(
      "Science Fiction & Speculative",
    );
    await switchLang(page, "en");
    await expect(page.getByRole("heading", { level: 1 })).toHaveText("Catalog");
  });
});
