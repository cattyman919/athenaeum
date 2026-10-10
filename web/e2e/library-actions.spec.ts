import { expect, test, type Page } from "@playwright/test";
import { ADMIN_STATE } from "./accounts";
import { gotoReady } from "./helpers";

// Mutating journeys run as the admin account, which starts with an empty library and no holds
// (docs/13 §6), so the demo-patron specs keep their seeded totals. Every test cleans up after
// itself so the spec can be re-run without re-seeding.
test.use({ storageState: ADMIN_STATE });

const ZERO_COPY = "The Elements of Style"; // docs/13 §5 zero-copy #34 (not_in_building)

async function openBook(page: Page, query: string, title: string) {
  await gotoReady(page, `/catalog?mode=keyword&q=${encodeURIComponent(query)}`);
  await page.getByTestId("book-card").filter({ hasText: title }).first().click();
  await expect(page).toHaveURL(/\/books\/[0-9a-f-]{36}$/);
  await page.locator("html[data-hydrated='true']").waitFor({ state: "attached" });
  await expect(page.getByTestId("book-title")).toHaveText(new RegExp(`^${title}`));
}

test.describe("book actions + waitlist (docs/03 §6.2–6.3, docs/07 §9)", () => {
  test("409 BOOK_NOT_AVAILABLE on a zero-copy book → join the waitlist", async ({ page }) => {
    await openBook(page, ZERO_COPY, ZERO_COPY);
    const banner = page.getByTestId("availability-banner");
    await expect(banner).toHaveAttribute("data-state", "not_in_building");
    await expect(banner.getByTestId("waitlist-join")).toBeVisible();

    // Marking it "Reading" trips the soft 409 → dialog with the suggested actions.
    await page.getByTestId("library-menu-trigger").click();
    await page.getByTestId("library-stage-reading").click();
    const dialog = page.getByTestId("unavailable-dialog");
    await expect(dialog).toContainText("This title isn't on the shelf");
    await expect(dialog.getByTestId("unavailable-add-anyway")).toBeVisible();
    await dialog.getByTestId("unavailable-join-waitlist").click();

    await expect(dialog).toBeHidden();
    await expect(page.getByText("You joined the waitlist")).toBeVisible();
    // Banner swaps to the hold position; nothing was added to the library.
    await expect(banner.getByTestId("waitlist-position")).toHaveText(
      "You are #1 in the waitlist queue",
    );
    await expect(page.getByTestId("library-actions")).toHaveAttribute("data-status", "");

    // The hold shows up on /library → Holds & Waitlists.
    await gotoReady(page, "/library?stage=holds");
    const hold = page.getByTestId("hold-row").filter({ hasText: ZERO_COPY });
    await expect(hold).toHaveCount(1);
    await expect(hold.getByTestId("hold-position")).toHaveText("Position 1 in queue");

    // Leave from the banner CTA (cleanup) → join button returns.
    await hold.getByRole("link", { name: ZERO_COPY }).click();
    await page.locator("html[data-hydrated='true']").waitFor({ state: "attached" });
    await page.getByTestId("waitlist-leave").click();
    await expect(page.getByText("You left the waitlist")).toBeVisible();
    await expect(banner.getByTestId("waitlist-join")).toBeVisible();
  });

  test("Add anyway (?force=true) then edit the row inline on /library", async ({ page }) => {
    await openBook(page, ZERO_COPY, ZERO_COPY);
    await page.getByTestId("library-menu-trigger").click();
    await page.getByTestId("library-stage-reading").click();
    await page.getByTestId("unavailable-add-anyway").click();
    await expect(page.getByTestId("library-actions")).toHaveAttribute("data-status", "reading");
    await expect(page.getByTestId("library-menu-stage")).toContainText("Reading");

    await gotoReady(page, "/library");
    const val = (k: string) => page.getByTestId(`kpi-${k}`).getByTestId("kpi-value");
    await expect(val("total")).toHaveText("1");
    await expect(val("reading")).toHaveText("1");
    const row = page.locator(`[data-title^='${ZERO_COPY}']`); // "… (4th ed.)"
    await expect(row.getByTestId("row-stage")).toHaveText("Reading");

    await row.getByTestId("row-stage-select").selectOption("read");
    await expect(row.getByTestId("row-stage")).toHaveText("Read");
    await expect(val("read")).toHaveText("1");

    await row.getByTestId("row-favorite-toggle").click();
    await expect(row.getByTestId("row-favorite")).toBeVisible();
    await expect(val("favorites")).toHaveText("1");

    await row.getByTestId("row-rating-4").click();
    await expect(row.getByTestId("row-rating-4")).toHaveAttribute("aria-pressed", "true");

    await row.getByTestId("row-remove").click();
    await expect(page.getByTestId("library-empty")).toBeVisible();
    await expect(val("total")).toHaveText("0");
  });

  test("available book: add, favorite, rate and remove from the detail menu", async ({ page }) => {
    await openBook(page, "dune", "Dune");
    const actions = page.getByTestId("library-actions");
    await expect(actions).toHaveAttribute("data-status", "");

    await page.getByTestId("library-menu-trigger").click();
    await page.getByTestId("library-stage-want_to_read").click();
    await expect(page.getByTestId("unavailable-dialog")).toBeHidden();
    await expect(actions).toHaveAttribute("data-status", "want_to_read");
    await expect(page.getByTestId("library-menu-stage")).toContainText("Want to Read");

    await page.getByTestId("library-favorite").click();
    await expect(page.getByTestId("library-favorite")).toHaveAttribute("aria-pressed", "true");
    await page.getByTestId("library-rating-5").click();
    await expect(page.getByTestId("library-rating-5")).toHaveAttribute("aria-pressed", "true");

    await page.getByTestId("library-menu-trigger").click();
    await page.getByTestId("library-remove").click();
    await expect(actions).toHaveAttribute("data-status", "");
    await expect(page.getByTestId("library-favorite")).toHaveCount(0);
  });

  test("zh-CN localizes the menu and waitlist CTA", async ({ page }) => {
    await openBook(page, ZERO_COPY, ZERO_COPY);
    await page.getByTestId("lang-zh-CN").click();
    await expect(page.getByTestId("library-menu-trigger")).toContainText("加入我的书架");
    await expect(page.getByTestId("waitlist-join")).toContainText("加入候补");
    await page.getByTestId("lang-en").click();
    await expect(page.getByTestId("library-menu-trigger")).toContainText("Add to My Library");
  });
});
