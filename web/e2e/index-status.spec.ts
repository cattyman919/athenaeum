import { expect, test, type Page, type Route } from "@playwright/test";
import { ADMIN_STATE, DEMO_STATE } from "./accounts";
import { gotoReady, switchLang } from "./helpers";
import { resetRateLimits } from "./rate-limit";

// Auto-index backlog UI (docs/07 §7 screens 2/6a/6b, docs/06 §2, docs/03 §5.1/§8.1).
// The e2e stack seeds + reindexes, so the real backlog is normally empty: these specs mock
// GET /api/admin/index-status and the catalog's keyword re-check with page.route and flip
// the mocked backlog to assert the badge / note appears and then clears.

test.beforeAll(async () => {
  await resetRateLimits();
});
const S2 = "MAIN/3F/SF/A-1/S-2"; // Dune C1/C2 + Neuromancer C1/C2 (docs/13 §5)
const DUNE_C1 = "ATH-002941-C01";
const NEUROMANCER_C1 = "ATH-002942-C01";

async function mockIndexStatus(page: Page, pending: { ids: string[] }) {
  let calls = 0;
  await page.route("**/api/admin/index-status", (route: Route) => {
    calls++;
    return route.fulfill({
      json: { pending_count: pending.ids.length, pending_book_ids: pending.ids },
    });
  });
  return { calls: () => calls };
}

test.describe("admin indexing badges (docs/07 screens 6a/6b)", () => {
  test.use({ storageState: ADMIN_STATE });

  test("manifest rows show Indexing… and the header chip until the backlog clears", async ({
    page,
  }) => {
    const tree = (await (await page.request.get("/api/admin/tree")).json()) as {
      floors: { sections: { aisles: { shelves: { id: string; path: string }[] }[] }[] }[];
    };
    const shelfId = tree.floors
      .flatMap((f) => f.sections.flatMap((s) => s.aisles.flatMap((a) => a.shelves)))
      .find((s) => s.path === S2)!.id;
    const shelf = (await (await page.request.get(`/api/admin/shelves/${shelfId}`)).json()) as {
      copies: { barcode: string; book_id: string }[];
    };
    const duneId = shelf.copies.find((c) => c.barcode === DUNE_C1)!.book_id;

    const pending = { ids: [duneId] };
    const status = await mockIndexStatus(page, pending);
    await gotoReady(page, `/admin/layout?shelf=${shelfId}`);

    const chip = page.getByTestId("index-status-chip");
    await expect(chip).toHaveText("1 book indexing");
    const row = (barcode: string) =>
      page.locator(`[data-testid=inspector-copy][data-barcode="${barcode}"]`);
    await expect(row(DUNE_C1).getByTestId("indexing-badge")).toHaveText("Indexing…");
    await expect(row(NEUROMANCER_C1).getByTestId("indexing-badge")).toHaveCount(0);

    pending.ids = []; // worker caught up → next 4 s poll clears it
    await expect(page.getByTestId("indexing-badge")).toHaveCount(0);
    await expect(chip).toHaveCount(0);

    // Polling stops once nothing is pending.
    const settled = status.calls();
    await page.waitForTimeout(5000);
    expect(status.calls()).toBe(settled);
  });

  test("a book added through Add Book is badged until it is indexed", async ({ page }) => {
    const pending = { ids: [] as string[] };
    await mockIndexStatus(page, pending);
    let bookId: string | null = null;
    // The new book is dirty as soon as POST /api/admin/books returns.
    await page.route("**/api/admin/books", async (route) => {
      if (route.request().method() !== "POST") return route.fallback();
      const res = await route.fetch();
      const body = (await res.json()) as { id?: string };
      if (body.id) {
        bookId = body.id;
        pending.ids = [body.id];
      }
      return route.fulfill({ response: res, json: body });
    });

    await gotoReady(page, "/admin");
    await expect(page.getByTestId("index-status-chip")).toHaveCount(0);
    await page.getByTestId("add-book-open").click();
    const dialog = page.getByTestId("add-book-dialog");
    await dialog.getByTestId("isbn-input").fill("9780553293357"); // e2e-stubs Foundation
    await dialog.getByTestId("isbn-lookup").click();
    await expect(dialog.getByLabel("Title")).toHaveValue("Foundation");
    await dialog.getByLabel("Section").selectOption("SF");
    await dialog.getByLabel("Shelf").selectOption({ index: 1 });
    await dialog.getByTestId("add-book-submit").click();
    await expect(dialog).toBeHidden();

    try {
      expect(bookId).not.toBeNull();
      const recent = page.locator(`[data-testid=recent-book][data-book-id="${bookId}"]`);
      await expect(recent).toContainText("Foundation");
      await expect(recent.getByTestId("indexing-badge")).toBeVisible();
      await expect(page.getByTestId("index-status-chip")).toHaveText("1 book indexing");

      pending.ids = [];
      await expect(recent.getByTestId("indexing-badge")).toHaveCount(0);
      await expect(page.getByTestId("index-status-chip")).toHaveCount(0);
    } finally {
      if (bookId) {
        const del = await page.request.delete(`/api/admin/books/${bookId}`);
        expect(del.status()).toBe(204);
      }
    }
  });
});

test.describe("/catalog indexing note (docs/07 screen 2)", () => {
  test.use({ storageState: DEMO_STATE });

  const isRecheck = (url: URL) =>
    url.pathname === "/api/books/search" &&
    url.searchParams.get("mode") === "keyword" &&
    url.searchParams.get("per_page") === "1";

  function mockBacklog(page: Page, backlog: { n: number }) {
    return page.route(isRecheck, (route) =>
      route.fulfill({
        json: {
          items: [],
          meta: { page: 1, per_page: 1, total: 0, total_pages: 0, index_pending: backlog.n },
        },
      }),
    );
  }

  test("semantic/hybrid results carry a non-blocking note while books are indexing", async ({
    page,
  }) => {
    const backlog = { n: 3 };
    await mockBacklog(page, backlog);
    await gotoReady(page, "/catalog?mode=hybrid&q=dune+desert+planet");

    const note = page.getByTestId("catalog-indexing-note");
    await expect(note).toHaveText(
      "3 new books are still being indexed — semantic results may be incomplete",
    );
    await expect(page.getByRole("status").filter({ has: note })).toHaveCount(1);
    // Non-blocking: results still render underneath.
    await expect(page.getByTestId("book-card").first()).toHaveAttribute("data-title", "Dune");

    backlog.n = 0;
    await expect(note).toHaveCount(0);
    await expect(page.getByTestId("book-card").first()).toHaveAttribute("data-title", "Dune");
  });

  test("keyword mode never shows the note", async ({ page }) => {
    let rechecked = false;
    await page.route(isRecheck, (route) => {
      rechecked = true;
      return route.fallback();
    });
    await gotoReady(page, "/catalog?q=dune");
    await expect(page.getByTestId("book-card").first()).toBeVisible();
    await expect(page.getByTestId("catalog-indexing-note")).toHaveCount(0);
    expect(rechecked).toBe(false);
  });

  test("the note is localized in zh-CN", async ({ page }) => {
    await mockBacklog(page, { n: 2 });
    await gotoReady(page, "/catalog?mode=semantic&q=dune");
    await switchLang(page, "zh-CN");
    await expect(page.getByTestId("catalog-indexing-note")).toHaveText(
      "2 本新书仍在建立索引——语义检索结果可能不完整",
    );
  });
});
