import { describe, expect, it, vi } from "vitest";
import { ApiError, createApiClient } from "./api-client";
import { EMPTY_FORM, buildCreateBody, describeIntakeError, metadataToForm } from "./intake";
import type { IsbnMetadata } from "./intake-types";

const foundation: IsbnMetadata = {
  isbn13: "9780553293357",
  title: "Foundation",
  authors: ["Isaac Asimov"],
  publisher: "Bantam Spectra",
  published_year: 1991,
  description: "The first novel.",
  cover_url: "https://covers.openlibrary.org/b/isbn/9780553293357-L.jpg",
  dewey_num: "813.54",
  loc_num: "PS3551.S5 F6 1991",
  open_library_key: "/books/OL7353617M",
  source: "openlibrary",
  in_catalog: false,
};

describe("metadataToForm", () => {
  it("prefills every field and keeps the chosen section", () => {
    const f = metadataToForm(foundation, { ...EMPTY_FORM, section: "SF", title: "old" });
    expect(f).toMatchObject({
      isbn13: "9780553293357",
      title: "Foundation",
      authors: "Isaac Asimov",
      publisher: "Bantam Spectra",
      published_year: "1991",
      dewey_num: "813.54",
      section: "SF",
    });
  });

  it("handles partial records", () => {
    const f = metadataToForm({
      ...foundation,
      title: "",
      authors: [],
      publisher: null,
      published_year: null,
      source: "offline_partial",
    });
    expect(f).toMatchObject({ title: "", authors: "", publisher: "", published_year: "" });
  });
});

describe("buildCreateBody (docs/03 §8.2)", () => {
  it("builds the admin create body with copies on one shelf", () => {
    const r = buildCreateBody(
      { ...metadataToForm(foundation), section: "SF", authors: "Isaac Asimov, ", publisher: " " },
      2,
      "MAIN/3F/SF/A-1/S-2",
    );
    expect(r.ok).toBe(true);
    if (!r.ok) return;
    expect(r.body).toMatchObject({
      isbn13: "9780553293357",
      title: "Foundation",
      authors: ["Isaac Asimov"],
      publisher: null,
      published_year: 1991,
      section: "SF",
      copies: [
        { status: "available", shelf_path: "MAIN/3F/SF/A-1/S-2" },
        { status: "available", shelf_path: "MAIN/3F/SF/A-1/S-2" },
      ],
    });
  });

  it("omits section and shelf when unset", () => {
    const r = buildCreateBody(metadataToForm(foundation), 1, "");
    expect(r.ok && r.body.section).toBeFalsy();
    expect(r.ok && r.body.copies).toEqual([{}]);
  });

  it("reports field errors", () => {
    const r = buildCreateBody(
      { ...EMPTY_FORM, isbn13: "036000291452", published_year: "19x1" },
      -1,
      "",
    );
    expect(r.ok).toBe(false);
    if (r.ok) return;
    expect(Object.keys(r.errors).sort()).toEqual(
      ["authors", "copies", "isbn13", "published_year", "title"].sort(),
    );
    expect(r.errors.isbn13).toMatch(/UPC/);
  });
});

describe("describeIntakeError (docs/11 §8)", () => {
  const err = (status: number, code: string, body: Record<string, unknown> = {}, h = {}) =>
    new ApiError(status, code, "msg", { error: code, ...body }, new Headers(h));

  it("409 ISBN_CONFLICT deep-links the existing book", () => {
    expect(describeIntakeError(err(409, "ISBN_CONFLICT", { book_id: "b1" }))).toMatchObject({
      bookId: "b1",
    });
  });

  it("400 INVALID_ISBN suggests manual entry", () => {
    expect(describeIntakeError(err(400, "INVALID_ISBN")).suggestManual).toBe(true);
  });

  it("429 shows the reset seconds from Retry-After or X-RateLimit-Reset", () => {
    expect(
      describeIntakeError(err(429, "RATE_LIMITED", {}, { "Retry-After": "42" })),
    ).toMatchObject({ retryAfter: 42, message: expect.stringContaining("42s") });
    const now = 1_000_000_000;
    expect(
      describeIntakeError(
        err(429, "RATE_LIMITED", {}, { "X-RateLimit-Reset": String(now / 1000 + 17) }),
        now,
      ).retryAfter,
    ).toBe(17);
  });

  it("non-API failures are network errors", () => {
    expect(describeIntakeError(new TypeError("fetch failed")).message).toMatch(/Network/);
  });
});

describe("api client intake calls", () => {
  it("POSTs fetch-isbn and surfaces ISBN_CONFLICT book_id", async () => {
    const fetcher = vi.fn(
      async () =>
        new Response(JSON.stringify({ error: "ISBN_CONFLICT", message: "x", book_id: "b9" }), {
          status: 409,
        }),
    );
    const api = createApiClient({ fetcher });
    const e = await api.fetchIsbn("9780441013593").catch((x: unknown) => x);
    expect(fetcher).toHaveBeenCalledWith(
      "/api/books/fetch-isbn",
      expect.objectContaining({ method: "POST", body: JSON.stringify({ isbn: "9780441013593" }) }),
    );
    expect(e).toBeInstanceOf(ApiError);
    expect(describeIntakeError(e).bookId).toBe("b9");
  });
});
