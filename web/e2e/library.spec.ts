import { expect, test } from "@playwright/test";
import { DEMO_STATE } from "./accounts";
import { gotoReady, switchLang } from "./helpers";
import { resetRateLimits } from "./rate-limit";

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
    await switchLang(page, "zh-CN");
    await expect(page.getByTestId("kpi-total")).toContainText("馆藏总数");
    await page.getByTestId("tab-reading").click();
    await expect(page.getByTestId("row-stage").first()).toHaveText("在读");
    await switchLang(page, "en");
  });
});

// Loans tab — borrow lifecycle: active loan → renew (once) → return.
// The test is self-contained: it borrows a copy via page.request (demo patron session)
// before navigating to the Loans tab, so it does not depend on seeded loans
// (the 12 seeded loans all belong to reviewer accounts — docs/13 §1/§3).
test.describe("/library Loans tab (docs/03 §6.5–6.7, docs/07 §library)", () => {
  // Ender's Game C1 — book #9, accession 002949; C1 is available (not in the checked-out
  // ledger, docs/13 §3).  Using a book that is not in the demo patron's library avoids KPI
  // side-effects on the other describe block's tests.
  const ENDERS_GAME_C1 = "ATH-002949-C01";

  test.beforeAll(async () => {
    // Reset rate-limit counters before the describe so the scan + borrow calls
    // (10/1min limit each) don't collide with counters from catalog.spec.ts or scanner.spec.ts.
    await resetRateLimits();
  });

  test("Loans tab shows an active loan; renew once; return the loan", async ({ page }) => {
    // ── Step 1: borrow a copy via the API so the test is fully self-contained. ──
    // Scan the copy tag to resolve the UUID (POST /api/books/scan, docs/03 §5.4).
    const scanRes = await page.request.post("/api/books/scan", {
      data: { code: ENDERS_GAME_C1 },
    });
    expect(scanRes.status(), await scanRes.text()).toBe(200);
    const scanData = (await scanRes.json()) as { copy?: { id?: string } };
    const copyId = scanData.copy?.id;
    expect(copyId, "scan must resolve copy UUID").toBeTruthy();

    // Borrow the copy (POST /api/copies/{id}/borrow, docs/03 §6.5).
    const borrowRes = await page.request.post(`/api/copies/${copyId}/borrow`);
    expect(borrowRes.status(), await borrowRes.text()).toBe(201);

    try {
      // ── Step 2: navigate to /library?stage=loans. ──
      await gotoReady(page, "/library?stage=loans");
      await expect(page.getByTestId("tab-loans")).toBeVisible();

      const loansList = page.getByTestId("loans-list");
      await expect(loansList).toBeVisible();

      // The newly created loan should appear as the first active row.
      const loanRow = page.getByTestId("loan-row").first();
      await expect(loanRow).toBeVisible();
      // Due date is displayed on the row (14 days from borrow, docs/03 §6.5).
      await expect(loanRow.getByTestId("loan-due")).toBeVisible();
      // Loan starts with renewals=0 so it is not overdue.
      await expect(loanRow.getByTestId("loan-overdue")).toHaveCount(0);

      // ── Step 3: renew once and assert the renew button disappears. ──
      const renewBtn = loanRow.getByTestId("loan-renew");
      // The renew button should be present (renewals=0, renewable=true).
      await expect(renewBtn).toBeVisible();

      const renewed = page.waitForResponse(
        (r) => /\/api\/copies\/[^/]+\/renew$/.test(r.url()) && r.request().method() === "POST",
      );
      await renewBtn.click();
      const renewApiRes = await renewed;
      expect(renewApiRes.status(), await renewApiRes.text()).toBe(200);
      // After one renewal the button must be gone (renewals=1, renewable=false, docs/03 §6.6).
      await expect(renewBtn).toHaveCount(0);

      // ── Step 4: return the loan via the UI. ──
      const returnBtn = loanRow.getByTestId("loan-return");
      await expect(returnBtn).toBeVisible();

      const returned = page.waitForResponse(
        (r) => /\/api\/copies\/[^/]+\/return$/.test(r.url()) && r.request().method() === "POST",
      );
      await returnBtn.click();
      const retApiRes = await returned;
      expect(retApiRes.status(), await retApiRes.text()).toBe(200);
      // After return, the return button must be gone from the row.
      await expect(returnBtn).toHaveCount(0);
    } finally {
      // Best-effort patron return via API so the copy goes back to available and the open
      // loan (if any) is closed — keeps the DB clean for repeated test runs.
      if (copyId) {
        await page.request.post(`/api/copies/${copyId}/return`).catch(() => {
          // 409 CONFLICT = copy already returned; nothing to do.
        });
      }
    }
  });
});
