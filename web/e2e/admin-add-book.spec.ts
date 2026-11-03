import { expect, test } from "@playwright/test";
import { ADMIN_STATE, DEMO_STATE } from "./accounts";
import { gotoReady } from "./helpers";

// Admin Add-Book dialog, ISBN manual-entry path (docs/11 §2/§8, docs/03 §5.3 + §8.2).
// Open Library is the e2e-stubs `/api/books` route (OPENLIBRARY_BASE_URL in
// playwright.config.ts), which only knows Foundation — absent from the seeded catalog
// (docs/13). The camera path is covered by vitest (lib/barcode-scanner.test.ts).
const STUB_ISBN = "9780553293357"; // api/cmd/e2e-stubs/openlibrary.go
const DUNE_ISBN = "9780441013593"; // seeded (docs/13 §2) → 409 ISBN_CONFLICT

test.describe("admin add book via ISBN (docs/11)", () => {
  test.use({ storageState: ADMIN_STATE });

  test("lookup prefills from Open Library and creates the book", async ({ page }) => {
    await gotoReady(page, "/admin");
    await page.getByTestId("add-book-open").click();
    const dialog = page.getByTestId("add-book-dialog");
    await expect(dialog.getByRole("tab", { name: "ISBN", exact: true })).toHaveAttribute(
      "data-state",
      "active",
    );

    await dialog.getByTestId("isbn-input").fill("978-0-553-29335-7");
    await dialog.getByTestId("isbn-lookup").click();

    // Prefilled Manual form for the cataloger to confirm.
    await expect(dialog.getByRole("tab", { name: "Manual" })).toHaveAttribute(
      "data-state",
      "active",
    );
    await expect(dialog.getByTestId("intake-cover-preview")).toBeVisible();
    await expect(dialog.getByLabel("ISBN-13")).toHaveValue(STUB_ISBN);
    await expect(dialog.getByLabel("Title")).toHaveValue("Foundation");
    await expect(dialog.getByLabel("Authors (comma-separated)")).toHaveValue("Isaac Asimov");
    await expect(dialog.getByLabel("Publisher")).toHaveValue("Bantam Spectra");
    await expect(dialog.getByLabel("Published year")).toHaveValue("1991");
    await dialog.getByText("Catalog numbers").click();
    await expect(dialog.getByLabel("Dewey")).toHaveValue("813.54");

    // Section and shelf are required: submitting without them shows inline errors.
    await dialog.getByTestId("add-book-submit").click();
    await expect(dialog.getByTestId("err-section")).toBeVisible();
    await expect(dialog.getByTestId("err-shelf")).toBeVisible();
    await dialog.getByLabel("Section").selectOption("SF");
    await dialog.getByLabel("Shelf").selectOption({ index: 1 });

    const created = page.waitForResponse(
      (r) => r.url().endsWith("/api/admin/books") && r.request().method() === "POST",
    );
    await dialog.getByTestId("add-book-submit").click();
    const res = await created;
    expect(res.status(), await res.text()).toBe(201);
    const book = (await res.json()) as { id: string };
    try {
      await expect(page.getByText("Added “Foundation” to the catalog.")).toBeVisible();
      await expect(dialog).toBeHidden();
    } finally {
      // Keep the seeded 39-title catalog intact for the other specs.
      const del = await page.request.delete(`/api/admin/books/${book.id}`);
      expect(del.status()).toBe(204);
    }
  });

  test("checksum failures are caught before the API is called", async ({ page }) => {
    await gotoReady(page, "/admin");
    await page.getByTestId("add-book-open").click();
    const dialog = page.getByTestId("add-book-dialog");
    let called = false;
    page.on("request", (r) => {
      if (r.url().includes("/api/books/fetch-isbn")) called = true;
    });
    await dialog.getByTestId("isbn-input").fill("9780553293358");
    await dialog.getByTestId("isbn-lookup").click();
    await expect(dialog.getByTestId("isbn-error")).toContainText("checksum");
    await dialog.getByTestId("isbn-input").fill("036000291452");
    await dialog.getByTestId("isbn-lookup").click();
    await expect(dialog.getByTestId("isbn-error")).toContainText("UPC");
    expect(called).toBe(false);
  });

  test("an ISBN already in the catalog deep-links to the book (409)", async ({ page }) => {
    await gotoReady(page, "/admin");
    await page.getByTestId("add-book-open").click();
    const dialog = page.getByTestId("add-book-dialog");
    await dialog.getByTestId("isbn-input").fill(DUNE_ISBN);
    await dialog.getByTestId("isbn-lookup").click();
    await expect(page.getByText("This book is already in the catalog.")).toBeVisible();
    await page.getByRole("button", { name: "Open in catalog" }).click();
    await expect(page).toHaveURL(/\/books\/[0-9a-f-]{36}$/);
    await expect(page.getByTestId("book-title")).toHaveText(/^Dune/);
  });
});

test.describe("admin gate", () => {
  test.use({ storageState: DEMO_STATE });

  test("patrons see the localized staff-only state", async ({ page }) => {
    await gotoReady(page, "/admin");
    await expect(page.getByTestId("admin-forbidden")).toBeVisible();
    await expect(page.getByTestId("add-book-open")).toHaveCount(0);
  });
});

test.describe("admin scan tab (docs/11 §3/§5)", () => {
  test.use({ storageState: ADMIN_STATE });

  test("scan tab loads the self-hosted ZXing WASM ponyfill and starts scanning", async ({
    page,
  }) => {
    // Force the barcode-detector ponyfill path (Safari/Firefox behaviour) on Chromium.
    await page.addInitScript(() => {
      delete (globalThis as { BarcodeDetector?: unknown }).BarcodeDetector;
    });
    const wasm = page.waitForResponse((r) =>
      /\/_next\/static\/media\/zxing_reader\.[0-9a-f]+\.wasm$/.test(r.url()),
    );
    let cdnHit = false;
    page.on("request", (r) => {
      if (r.url().includes("jsdelivr")) cdnHit = true;
    });

    await gotoReady(page, "/admin");
    await page.getByTestId("add-book-open").click();
    const dialog = page.getByTestId("add-book-dialog");
    await dialog.getByRole("tab", { name: "Scan ISBN" }).click();

    const scanner = dialog.getByTestId("isbn-scanner");
    await expect(scanner).toBeVisible();
    const res = await wasm;
    expect(res.status()).toBe(200);
    expect(res.headers()["content-type"]).toContain("application/wasm");
    await expect(scanner).toHaveAttribute("data-status", "scanning");
    // The first detect() compiles the module; a load failure would flip to the wasm error card.
    await page.waitForTimeout(1500);
    await expect(scanner).toHaveAttribute("data-status", "scanning");
    await expect(dialog.getByTestId("scanner-error")).toHaveCount(0);
    expect(cdnHit).toBe(false);
  });
});
