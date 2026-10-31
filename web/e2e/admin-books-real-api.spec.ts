import { randomUUID } from "node:crypto";
import { expect, test } from "@playwright/test";
import { ADMIN_STATE } from "./accounts";
import { gotoReady } from "./helpers";

test.use({ storageState: ADMIN_STATE });

test("searches and removes disposable books while protecting a checked-out copy", async ({
  page,
}) => {
  test.setTimeout(60_000);
  const prefix = `E2E Admin ${randomUUID()}`;
  const createdIds: string[] = [];
  let borrowedCopyId: string | undefined;

  async function createBook(title: string) {
    const response = await page.request.post("/api/admin/books", {
      data: { title, authors: ["E2E Author"], copies: [{}] },
    });
    expect(response.status(), await response.text()).toBe(201);
    const book = (await response.json()) as { id: string; title: string; copies: { id: string }[] };
    createdIds.push(book.id);
    expect(book.copies).toHaveLength(1);
    return book;
  }

  try {
    const first = await createBook(`${prefix} Alpha`);
    const second = await createBook(`${prefix} Beta`);
    const loaned = await createBook(`${prefix} Loaned`);
    const copyId = loaned.copies[0].id;
    const borrow = await page.request.post(`/api/copies/${copyId}/borrow`);
    expect(borrow.status(), await borrow.text()).toBe(201);
    borrowedCopyId = copyId;

    await gotoReady(page, "/admin");
    const panel = page.getByTestId("admin-books-table");
    await panel.getByTestId("admin-books-search").fill(prefix);
    const row = (id: string) =>
      panel.locator(`[data-testid="admin-book-row"][data-book-id="${id}"]`);
    await expect(row(first.id)).toContainText(first.title);
    await expect(row(second.id)).toContainText(second.title);
    await expect(row(loaned.id)).toContainText(loaned.title);

    await row(loaned.id).getByTestId("admin-book-select").check();
    await panel.getByTestId("admin-books-remove-selected").click();
    const dialog = page.getByTestId("remove-book-confirm");
    await expect(dialog).toContainText(loaned.title);
    await expect(dialog).toContainText("1 active loans");
    await expect(dialog.getByTestId("confirm-accept")).toBeDisabled();
    await dialog.getByTestId("confirm-cancel").click();
    await panel.getByTestId("admin-books-clear-selection").click();

    const blocked = await page.request.delete(`/api/admin/books/${loaned.id}`);
    expect(blocked.status(), await blocked.text()).toBe(409);
    expect((await page.request.get(`/api/books/${loaned.id}`)).status()).toBe(200);

    await row(first.id).getByTestId("admin-book-select").check();
    await row(second.id).getByTestId("admin-book-select").check();
    await panel.getByTestId("admin-books-remove-selected").click();
    await expect(dialog).toContainText(first.title);
    await expect(dialog).toContainText(second.title);
    const deleted = page.waitForResponse(
      (response) =>
        response.url().endsWith("/api/admin/books/bulk-delete") &&
        response.request().method() === "POST",
    );
    await dialog.getByTestId("confirm-accept").click();
    const deletion = await deleted;
    expect(deletion.status()).toBe(204);
    await expect(row(first.id)).toHaveCount(0);
    await expect(row(second.id)).toHaveCount(0);
    expect((await page.request.get(`/api/books/${first.id}`)).status()).toBe(404);
    expect((await page.request.get(`/api/books/${second.id}`)).status()).toBe(404);
    expect((await page.request.get(`/api/books/${loaned.id}`)).status()).toBe(200);
  } finally {
    const cleanupErrors: string[] = [];
    if (borrowedCopyId) {
      const returned = await page.request.post(`/api/copies/${borrowedCopyId}/return`);
      if (returned.status() !== 200)
        cleanupErrors.push(`Return: ${returned.status()} ${await returned.text()}`);
    }
    for (const id of createdIds) {
      const response = await page.request.delete(`/api/admin/books/${id}`);
      if (![204, 404].includes(response.status())) {
        cleanupErrors.push(`Delete ${id}: ${response.status()} ${await response.text()}`);
      }
    }
    expect(cleanupErrors).toEqual([]);
  }
});
