import { expect, test } from "@playwright/test";
import { ADMIN_STATE } from "./accounts";
import { gotoReady } from "./helpers";
import { resetRateLimits } from "./rate-limit";

// RealTimeAuditStream (docs/03 §8.1, docs/07 screen 6a): a copy status change made while
// /admin is open shows up through the 3 s `since` poll without a reload. The copy is
// restored afterwards so the seeded counts (docs/13 §1) hold for the other specs.
const BARCODE = "ATH-002942-C02"; // Neuromancer C2, available (docs/13 §3)

test.describe("admin audit stream", () => {
  test.use({ storageState: ADMIN_STATE });
  test.beforeAll(async () => {
    await resetRateLimits();
  });

  test("a status change appears in the live stream", async ({ page }) => {
    await gotoReady(page, "/admin");
    const stream = page.getByTestId("audit-stream");
    await expect(stream.getByTestId("audit-live")).toHaveAttribute("data-state", "live");

    const maxId = async () =>
      Math.max(
        0,
        ...(await stream
          .getByTestId("audit-row")
          .evaluateAll((rows) => rows.map((r) => Number(r.getAttribute("data-event-id"))))),
      );
    const baseline = await maxId();

    // Only polls issued after the change can carry the new event.
    const polled = page.waitForResponse(
      (r) => r.url().includes("/api/admin/copy-events?") && r.url().includes("since="),
      { timeout: 15_000 },
    );
    try {
      const res = await page.request.patch(`/api/admin/copies/${BARCODE}`, {
        data: { status: "missing_lost" },
      });
      expect(res.status(), await res.text()).toBe(200);

      const rows = stream.locator(
        `[data-testid=audit-row][data-barcode="${BARCODE}"][data-to="missing_lost"]`,
      );
      // Newest first: the first matching row must be a fresh event id.
      await expect
        .poll(
          async () =>
            Math.max(
              0,
              ...(await rows.evaluateAll((els) =>
                els.map((el) => Number(el.getAttribute("data-event-id"))),
              )),
            ),
          { timeout: 10_000 },
        )
        .toBeGreaterThan(baseline);
      const row = rows.first();
      await expect(row).toContainText("Neuromancer");
      await expect(row).toContainText("admin_patch");
      await expect(row.getByTestId("audit-to")).toHaveText("Missing / lost");
      await polled;
    } finally {
      const res = await page.request.patch(`/api/admin/copies/${BARCODE}`, {
        data: { status: "available" },
      });
      expect(res.status()).toBe(200);
    }
    // The revert is streamed too.
    await expect(
      stream
        .locator(`[data-testid=audit-row][data-barcode="${BARCODE}"][data-to="available"]`)
        .first(),
    ).toBeVisible({ timeout: 10_000 });
  });
});
