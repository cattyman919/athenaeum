import { expect, test } from "@playwright/test";
import { ADMIN_STATE } from "./accounts";
import { gotoReady } from "./helpers";

const book = (id: string, title: string, removable = true) => ({
  id,
  title,
  authors: ["Author"],
  isbn13: "9780000000000",
  section: null,
  availability: { total_copies: 1, available_copies: 1, state: "available" },
  updated_at: "2026-01-01T00:00:00Z",
  active_loans: removable ? 0 : 1,
  removable,
});

const alpha = book("a", "Alpha");
const beta = book("b", "Beta");
const loaned = book("c", "Loaned", false);

test.use({ storageState: ADMIN_STATE });

test("browses, searches, paginates and handles blocked and bulk removal", async ({ page }) => {
  let failure = true;
  let conflict = true;
  let removed = false;
  await page.route("**/api/admin/books**", async (route) => {
    const url = new URL(route.request().url());
    if (route.request().method() === "POST") {
      expect(route.request().postDataJSON()).toEqual({ ids: ["a", "b"] });
      if (conflict) {
        conflict = false;
        await route.fulfill({
          status: 409,
          json: {
            error: "CONFLICT",
            message: "Blocked",
            blocked: [{ id: "b", title: "Beta", active_loans: 1 }],
          },
        });
      } else {
        removed = true;
        await route.fulfill({ status: 204 });
      }
      return;
    }
    if (failure) {
      failure = false;
      await route.fulfill({ status: 500, json: { error: "INTERNAL" } });
      return;
    }
    const search = url.searchParams.get("search") ?? "";
    const current = Number(url.searchParams.get("page") ?? 1);
    const items = search
      ? search.toLowerCase() === "loaned"
        ? [loaned]
        : []
      : current === 2
        ? [loaned]
        : removed
          ? []
          : [alpha, beta];
    await route.fulfill({
      json: {
        items,
        meta: {
          page: current,
          per_page: 20,
          total: search ? items.length : removed ? 0 : 3,
          total_pages: search ? 1 : removed ? 0 : 2,
        },
      },
    });
  });
  await gotoReady(page, "/admin");
  const panel = page.getByTestId("admin-books-table");
  await expect(panel.getByTestId("admin-books-error")).toContainText("Could not load books");
  await panel.getByRole("button", { name: "Retry" }).click();
  await expect(panel.getByText("Alpha", { exact: true })).toBeVisible();
  await panel.getByRole("button", { name: "Next" }).click();
  await expect(panel.getByText("Loaned", { exact: true })).toBeVisible();
  await panel.getByRole("button", { name: "Remove Loaned" }).click();
  const dialog = page.getByRole("alertdialog");
  await expect(dialog).toContainText("1 active loans");
  await expect(dialog.getByRole("button", { name: "Remove permanently" })).toBeDisabled();
  await dialog.getByTestId("confirm-cancel").click();
  await panel.getByRole("button", { name: "Previous" }).click();
  await panel.getByTestId("admin-books-select-all").check();
  await expect(panel.getByTestId("admin-book-select")).toHaveCount(2);
  await panel.getByTestId("admin-books-clear-selection").click();
  await expect(panel.getByTestId("admin-books-select-all")).not.toBeChecked();
  await panel.getByTestId("admin-books-select-all").check();
  await panel.getByTestId("admin-books-remove-selected").click();
  await expect(page.getByTestId("remove-book-confirm")).toContainText("cannot be undone");
  await expect(dialog.getByRole("list", { name: "Books selected for removal" })).toContainText(
    "Alpha",
  );
  await expect(dialog.getByRole("list", { name: "Books selected for removal" })).toContainText(
    "9780000000000",
  );
  await dialog.getByRole("button", { name: "Remove permanently" }).click();
  await expect(panel.getByRole("alert")).toContainText("Beta (1 active loans)");
  await expect(panel.getByText("Alpha", { exact: true })).toBeVisible();
  await panel.getByRole("button", { name: "Remove selected (2)" }).click();
  await dialog.getByRole("button", { name: "Remove permanently" }).click();
  await expect(panel.getByText("No books in the catalog.")).toBeVisible();
  await expect(page.getByText("Removed 2 books.")).toBeVisible();
  await panel.getByLabel("Search books by title, author or ISBN").fill("unknown");
  await expect(panel.getByText("No books match “unknown”.")).toBeVisible();
  await panel.getByTestId("admin-books-clear-search").click();
  await expect(panel.getByLabel("Search books by title, author or ISBN")).toHaveValue("");
  await expect(panel.getByText("No books in the catalog.")).toBeVisible();
});

test("single removal requires confirmation and supports keyboard cancellation", async ({
  page,
}) => {
  let removed = false;
  await page.route("**/api/admin/books**", async (route) => {
    if (route.request().method() === "DELETE") {
      expect(new URL(route.request().url()).pathname).toBe("/api/admin/books/a");
      removed = true;
      await route.fulfill({ status: 204 });
      return;
    }
    const items = removed ? [] : [alpha];
    await route.fulfill({
      json: { items, meta: { page: 1, per_page: 20, total: items.length, total_pages: 1 } },
    });
  });
  await gotoReady(page, "/admin");
  const panel = page.getByTestId("admin-books-table");
  const remove = panel.getByRole("button", { name: "Remove Alpha" });
  await expect(panel.getByTestId("book-merge")).toBeVisible();
  await remove.focus();
  await page.keyboard.press("Enter");
  await expect(page.getByTestId("confirm-cancel")).toBeFocused();
  await page.keyboard.press("Escape");
  await expect(page.getByRole("alertdialog")).toHaveCount(0);
  await expect(remove).toBeFocused();
  expect(removed).toBe(false);
  await remove.click();
  await page.getByRole("button", { name: "Remove permanently" }).click();
  await expect(panel.getByText("No books in the catalog.")).toBeVisible();
});

test("confirmation identifies at most five selected titles and describes the remainder", async ({
  page,
}) => {
  const items = Array.from({ length: 7 }, (_, index) => book(String(index), `Title ${index + 1}`));
  await page.route("**/api/admin/books**", async (route) => {
    await route.fulfill({
      json: { items, meta: { page: 1, per_page: 20, total: items.length, total_pages: 1 } },
    });
  });
  await gotoReady(page, "/admin");
  await page.getByTestId("admin-books-select-all").check();
  await page.getByTestId("admin-books-remove-selected").click();
  const dialog = page.getByTestId("remove-book-confirm");
  const list = dialog.getByRole("list", { name: "Books selected for removal" });
  await expect(list.getByRole("listitem")).toHaveCount(5);
  await expect(list).toContainText("Title 1");
  await expect(list).toContainText("Author");
  await expect(list).toContainText("ISBN: 9780000000000");
  await expect(list).toContainText("1 copies");
  await expect(dialog).toContainText("+2 more");
  await expect(dialog).toHaveAttribute("aria-describedby", /.+/);
  const described = await dialog.evaluate((el) =>
    (el.getAttribute("aria-describedby") ?? "")
      .split(" ")
      .map((id) => document.getElementById(id)?.textContent)
      .join(" "),
  );
  expect(described).toContain("Title 1");
  expect(described).toContain("cannot be undone");
});

test("ignores an older search response after a newer search", async ({ page }) => {
  await page.route("**/api/admin/books**", async (route) => {
    const search = new URL(route.request().url()).searchParams.get("search") ?? "";
    if (search === "slow") await new Promise((resolve) => setTimeout(resolve, 1200));
    const items = search === "slow" ? [alpha] : search === "fast" ? [beta] : [];
    await route
      .fulfill({
        json: { items, meta: { page: 1, per_page: 20, total: items.length, total_pages: 1 } },
      })
      .catch(() => {});
  });
  await gotoReady(page, "/admin");
  const input = page.getByLabel("Search books by title, author or ISBN");
  await input.fill("slow");
  await page.waitForRequest((request) => request.url().includes("search=slow"));
  await input.fill("fast");
  await expect(
    page.getByTestId("admin-books-table").getByText("Beta", { exact: true }),
  ).toBeVisible();
  await page.waitForTimeout(1300);
  await expect(
    page.getByTestId("admin-books-table").getByText("Alpha", { exact: true }),
  ).toHaveCount(0);
});
