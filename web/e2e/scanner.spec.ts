import { expect, test, type Page } from "@playwright/test";
import { ADMIN_STATE, DEMO_STATE } from "./accounts";
import { gotoReady } from "./helpers";

// Universal barcode scanner — manual code path (docs/11 §2/§8, docs/03 §5.4 + §6.4,
// docs/09 §8). The camera decode loop stays unit-tested (lib/barcode-scanner.test.ts).
const DUNE_ISBN = "9780441013593"; // #1, 3 copies, C01 checked_out (docs/13 §2/§3)
const NOT_SEEDED_ISBN = "9780553293357"; // valid ISBN-13 outside the seeded catalog
const KR_C1 = "ATH-002960-C01"; // #20 The C Programming Language C1, checked_out (docs/13 §3)

async function openScanner(page: Page) {
  await page.getByTestId("open-scanner").click();
  const dialog = page.getByTestId("scanner-dialog");
  await expect(dialog).toBeVisible();
  return dialog;
}

async function lookup(page: Page, code: string) {
  const dialog = page.getByTestId("scanner-dialog");
  await dialog.getByTestId("scanner-manual-input").fill(code);
  await dialog.getByTestId("scanner-manual-submit").click();
}

test.describe("patron barcode scanner (docs/11)", () => {
  test.use({ storageState: DEMO_STATE });

  test("ISBN lookup shows the record, shelf ladder and copies left", async ({ page }) => {
    await gotoReady(page, "/catalog");
    const dialog = await openScanner(page);
    // Patron Lookup is the only mode for non-admins.
    await expect(dialog.getByTestId("scanner-tab-staff")).toHaveCount(0);

    const scanned = page.waitForResponse(
      (r) => r.url().endsWith("/api/books/scan") && r.request().method() === "POST",
    );
    await lookup(page, "978-0-441-01359-3");
    const res = await scanned;
    expect(res.status(), await res.text()).toBe(200);
    expect(JSON.parse(res.request().postData() ?? "{}")).toEqual({ code: DUNE_ISBN });

    const result = dialog.getByTestId("scan-result");
    await expect(result).toHaveAttribute("data-match", "book");
    await expect(result.getByTestId("scan-result-title")).toHaveText("Dune");
    await expect(result.getByTestId("scan-availability")).toHaveAttribute("data-available", "2");
    await expect(result.getByTestId("scan-availability")).toContainText("2 copies on shelf");
    await expect(result.getByTestId("scan-path")).toHaveText("MAIN/3F/SF/A-1/S-2");
    await expect(result.getByTestId("scan-ladder")).toContainText("Aisle A-1");
    await expect(dialog.getByTestId("scanner-status")).toContainText(DUNE_ISBN);

    await dialog.getByTestId("scan-view-record").click();
    await expect(page).toHaveURL(/\/books\/[^/]+$/);
    await expect(dialog).toBeHidden();
    await expect(page.getByTestId("book-title")).toHaveText("Dune");
  });

  test("client-side checks catch bad codes before the API is called", async ({ page }) => {
    await gotoReady(page, "/catalog");
    const dialog = await openScanner(page);
    let called = false;
    page.on("request", (r) => {
      if (r.url().includes("/api/books/scan")) called = true;
    });
    const hint = dialog.getByTestId("scanner-manual-error");

    await lookup(page, "9780441013594"); // checksum failure
    await expect(hint).toContainText("Barcode unusable");
    await lookup(page, "0441013595"); // ISBN-10
    await expect(hint).toContainText("ISBN-10 is not supported");
    await lookup(page, "036000291452"); // UPC store barcode
    await expect(hint).toContainText("UPC");
    expect(called).toBe(false);
  });

  test("a valid ISBN outside the catalog shows the not-in-catalog card", async ({ page }) => {
    await gotoReady(page, "/catalog");
    const dialog = await openScanner(page);
    await lookup(page, NOT_SEEDED_ISBN);
    const card = dialog.getByTestId("scan-not-in-catalog");
    await expect(card).toContainText("Not in this building's catalog");
    await expect(card).toContainText(NOT_SEEDED_ISBN);
    await expect(card.getByTestId("scan-staff-intake")).toHaveCount(0);
  });

  test("scan to return: a checked-out copy tag is returned from /library", async ({
    page,
    browser,
  }) => {
    await gotoReady(page, "/library");
    await page.getByTestId("library-scan-return").click();
    const dialog = page.getByTestId("scanner-dialog");
    await expect(dialog).toBeVisible();

    try {
      await lookup(page, KR_C1.toLowerCase());
      const result = dialog.getByTestId("scan-result");
      await expect(result).toHaveAttribute("data-match", "copy");
      await expect(result.getByTestId("scan-result-title")).toHaveText(
        "The C Programming Language",
      );
      await expect(result.getByTestId("scan-copy")).toContainText(KR_C1);
      await expect(result.getByTestId("scan-copy-status")).toHaveAttribute(
        "data-status",
        "checked_out",
      );

      const returned = page.waitForResponse(
        (r) => /\/api\/copies\/[^/]+\/return$/.test(r.url()) && r.request().method() === "POST",
      );
      await result.getByTestId("scan-return").click();
      const res = await returned;
      expect(res.status(), await res.text()).toBe(200);

      await expect(page.getByText(`Returned ${KR_C1} — thank you!`)).toBeVisible();
      await expect(result.getByTestId("scan-copy-status")).toHaveAttribute(
        "data-status",
        "available",
      );
      await expect(result.getByTestId("scan-return")).toHaveCount(0);
      await expect(result.getByTestId("scan-not-returnable")).toBeVisible();
      await expect(result.getByTestId("scan-availability")).toHaveAttribute("data-available", "2");

      // Scan Next clears the result and keeps the dialog open.
      await dialog.getByTestId("scan-next").click();
      await expect(dialog.getByTestId("scan-result")).toHaveCount(0);
      await expect(dialog.getByTestId("scanner-manual-input")).toBeFocused();
    } finally {
      // Restore the seeded circulation state (docs/13 §3) for the other specs.
      const admin = await browser.newContext({ storageState: ADMIN_STATE });
      const res = await admin.request.patch(`/api/admin/copies/${KR_C1}`, {
        data: { status: "checked_out" },
      });
      expect(res.status(), await res.text()).toBe(200);
      await admin.close();
    }
  });
});

test.describe("admin scanner (docs/07 §Scanner dialog)", () => {
  test.use({ storageState: ADMIN_STATE });

  test("/admin opens the shared dialog on Staff Intake", async ({ page }) => {
    await gotoReady(page, "/admin");
    await page.getByTestId("admin-open-intake").click();
    const dialog = page.getByTestId("scanner-dialog");
    await expect(dialog.getByTestId("scanner-tab-staff")).toHaveAttribute("data-state", "active");
    const panel = dialog.getByTestId("staff-intake-panel");
    await expect(panel.getByTestId("isbn-input")).toBeVisible();

    // The nav Scan button still defaults to Patron Lookup for admins.
    await page.keyboard.press("Escape");
    await expect(dialog).toBeHidden();
    await page.getByTestId("open-scanner").click();
    await expect(dialog.getByTestId("scanner-tab-patron")).toHaveAttribute("data-state", "active");
  });

  test("an unknown ISBN hands over to Staff Intake", async ({ page }) => {
    await gotoReady(page, "/catalog");
    await page.getByTestId("open-scanner").click();
    const dialog = page.getByTestId("scanner-dialog");
    await lookup(page, NOT_SEEDED_ISBN);
    const fetched = page.waitForResponse((r) => r.url().endsWith("/api/books/fetch-isbn"));
    await dialog.getByTestId("scan-staff-intake").click();
    await expect(dialog.getByTestId("scanner-tab-staff")).toHaveAttribute("data-state", "active");
    expect((await fetched).status()).toBe(200);
    await expect(dialog.getByText("Prefilled from Open Library")).toBeVisible();
  });
});
