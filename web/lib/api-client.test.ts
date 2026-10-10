import { describe, expect, it, vi } from "vitest";
import { ApiError, createApiClient } from "./api-client";

const ok = (body: unknown, status = 200) =>
  new Response(JSON.stringify(body), { status, headers: { "Content-Type": "application/json" } });

describe("api client refresh retry (docs/07 §4)", () => {
  it("retries once after a 401 when refresh succeeds", async () => {
    const calls: string[] = [];
    let first = true;
    const fetcher = vi.fn(async (input: RequestInfo | URL) => {
      const url = String(input);
      calls.push(url);
      if (url === "/api/auth/refresh") return ok({});
      if (first) {
        first = false;
        return ok({ error: "UNAUTHENTICATED", message: "x" }, 401);
      }
      return ok({ items: [] });
    });
    const expired = vi.fn();
    const c = createApiClient({ fetcher: fetcher as unknown as typeof fetch, onExpired: expired });
    expect(await c.json("/api/library")).toEqual({ items: [] });
    expect(calls).toEqual(["/api/library", "/api/auth/refresh", "/api/library"]);
    expect(expired).not.toHaveBeenCalled();
  });

  it("calls onExpired and throws on the second 401", async () => {
    const fetcher = vi.fn(async (input: RequestInfo | URL) =>
      String(input) === "/api/auth/refresh"
        ? ok({})
        : ok({ error: "UNAUTHENTICATED", message: "no" }, 401),
    );
    const expired = vi.fn();
    const c = createApiClient({ fetcher: fetcher as unknown as typeof fetch, onExpired: expired });
    await expect(c.json("/api/library")).rejects.toMatchObject({
      status: 401,
      code: "UNAUTHENTICATED",
    });
    expect(expired).toHaveBeenCalledOnce();
    expect(fetcher).toHaveBeenCalledTimes(3);
  });

  it("does not retry when refresh itself fails", async () => {
    const fetcher = vi.fn(async (input: RequestInfo | URL) =>
      String(input) === "/api/auth/refresh"
        ? ok({}, 401)
        : ok({ error: "UNAUTHENTICATED", message: "no" }, 401),
    );
    const expired = vi.fn();
    const c = createApiClient({ fetcher: fetcher as unknown as typeof fetch, onExpired: expired });
    await expect(c.json("/api/library")).rejects.toBeInstanceOf(ApiError);
    expect(fetcher).toHaveBeenCalledTimes(2);
    expect(expired).toHaveBeenCalledOnce();
  });

  it("never refresh-retries /api/auth/* (login errors surface directly)", async () => {
    const fetcher = vi.fn(async () => ok({ error: "INVALID_CREDENTIALS", message: "bad" }, 401));
    const c = createApiClient({ fetcher: fetcher as unknown as typeof fetch });
    await expect(c.login("a@b.co", "pw")).rejects.toMatchObject({ code: "INVALID_CREDENTIALS" });
    expect(fetcher).toHaveBeenCalledTimes(1);
  });

  it("de-duplicates concurrent refreshes", async () => {
    let refreshCalls = 0;
    const seen = new Set<string>();
    const fetcher = vi.fn(async (input: RequestInfo | URL) => {
      const url = String(input);
      if (url === "/api/auth/refresh") {
        refreshCalls++;
        await new Promise((r) => setTimeout(r, 5));
        return ok({});
      }
      if (!seen.has(url)) {
        seen.add(url);
        return ok({ error: "UNAUTHENTICATED", message: "x" }, 401);
      }
      return ok({ ok: true });
    });
    const c = createApiClient({ fetcher: fetcher as unknown as typeof fetch });
    await Promise.all([c.json("/api/a"), c.json("/api/b"), c.json("/api/c")]);
    expect(refreshCalls).toBe(1);
  });

  it("maps error envelopes and 204", async () => {
    const fetcher = vi.fn(async (input: RequestInfo | URL) =>
      String(input).endsWith("logout")
        ? new Response(null, { status: 204 })
        : ok({ error: "EMAIL_TAKEN", message: "taken" }, 409),
    );
    const c = createApiClient({ fetcher: fetcher as unknown as typeof fetch });
    await expect(c.register("N", "a@b.co", "pppppppppp")).rejects.toMatchObject({
      status: 409,
      code: "EMAIL_TAKEN",
      message: "taken",
    });
    await expect(c.logout()).resolves.toBeUndefined();
  });
});

describe("admin client (docs/03 §8)", () => {
  it("builds the copy-events cursor query and unwraps items", async () => {
    const urls: string[] = [];
    const fetcher = vi.fn(async (input: RequestInfo | URL) => {
      urls.push(String(input));
      return ok({ items: [{ id: 1 }] });
    });
    const c = createApiClient({ fetcher: fetcher as unknown as typeof fetch });
    expect(await c.adminCopyEvents(null)).toEqual([{ id: 1 }]);
    await c.adminCopyEvents("2026-10-06T19:52:03.123456Z", 20);
    expect(urls).toEqual([
      "/api/admin/copy-events?limit=50",
      "/api/admin/copy-events?limit=20&since=2026-10-06T19%3A52%3A03.123456Z",
    ]);
  });

  it("patches copies by barcode with a JSON body", async () => {
    const fetcher = vi.fn(async () => ok({ id: "c1" }));
    const c = createApiClient({ fetcher: fetcher as unknown as typeof fetch });
    await c.patchAdminCopy("ATH-002941-C02", { shelf_id: "s1" });
    const [url, init] = fetcher.mock.calls[0] as unknown as [string, RequestInit];
    expect(url).toBe("/api/admin/copies/ATH-002941-C02");
    expect(init.method).toBe("PATCH");
    expect(JSON.parse(String(init.body))).toEqual({ shelf_id: "s1" });
    expect(new Headers(init.headers).get("Content-Type")).toBe("application/json");
  });

  it("surfaces the self-demote 400 as an ApiError", async () => {
    const fetcher = vi.fn(async () =>
      ok({ error: "VALIDATION_FAILED", message: "You cannot demote yourself." }, 400),
    );
    const c = createApiClient({ fetcher: fetcher as unknown as typeof fetch });
    await expect(c.patchAdminUser("me", "user")).rejects.toBeInstanceOf(ApiError);
  });
});
