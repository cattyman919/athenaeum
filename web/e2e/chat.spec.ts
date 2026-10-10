import { expect, test } from "@playwright/test";
import { DEMO_STATE } from "./accounts";
import { gotoReady } from "./helpers";

// AI Librarian chat (docs/06 §6, docs/07 §6–7, docs/14 §10). The API runs the real agent,
// tools and SSE pipeline against api/cmd/e2e-stubs: the stub model issues one
// search_catalog call with the longest word of the message, then streams an answer naming
// the first hit (in Simplified Chinese when the request locale is zh-CN).

test.use({ storageState: DEMO_STATE });

const WEB_URL = "http://localhost:13000";
const COVER_URL = /^https:\/\/covers\.openlibrary\.org\/b\/isbn\/\d{13}-[SML]\.jpg$/;
// 1×1 transparent PNG served for Open Library covers so the thumbnail loads offline.
const PIXEL = Buffer.from(
  "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mNkYPhfDwAChwGA60e6kgAAAABJRU5ErkJggg==",
  "base64",
);

test.describe("delete conversation confirmation", () => {
  test("asks first; cancel keeps the chat, confirm deletes it", async ({ page }) => {
    await gotoReady(page, "/chat");
    await page.getByTestId("chat-composer").fill("Recommend Hyperion please");
    await page.getByTestId("chat-send").click();
    await expect(page.getByTestId("chat-assistant-message")).toBeVisible();
    const item = page.getByTestId("conversation-item").first();
    await expect(item).toBeVisible();
    const before = await page.getByTestId("conversation-item").count();

    const del = item.getByRole("button", { name: /^Delete conversation/ });
    await item.hover();
    await del.click();
    const dialog = page.getByTestId("confirm-dialog");
    await expect(dialog).toContainText("Delete this conversation?");
    await page.getByTestId("confirm-cancel").click();
    await expect(dialog).toBeHidden();
    await expect(item).toBeVisible();

    await item.hover();
    await del.click();
    await page.getByTestId("confirm-accept").click();
    await expect(page.getByText("Conversation deleted")).toBeVisible();
    await expect(page.getByTestId("conversation-item")).toHaveCount(before - 1);
  });
});

test.describe("AI reply markdown", () => {
  test("renders headings, lists, bold, code and tables; escapes raw HTML", async ({ page }) => {
    const md = [
      "## Where to look",
      "",
      "*Dune* is on **Floor 3**:",
      "",
      "- Aisle `A-1`",
      "- Shelf S-2",
      "",
      "| Copy | State |",
      "| --- | --- |",
      "| C01 | available |",
      "",
      "<script>window.__pwned = true</script>",
      "",
      "[Open the catalog](/catalog)",
    ].join("\n");
    const sse = [
      `event: meta\ndata: ${JSON.stringify({ conversation_id: "11111111-1111-4111-8111-111111111111", turn: 1, model: "stub", locale: "en" })}\n\n`,
      `event: token\ndata: ${JSON.stringify({ text: md })}\n\n`,
      `event: citations\ndata: ${JSON.stringify({ citations: [] })}\n\n`,
      `event: done\ndata: ${JSON.stringify({ message_id: "22222222-2222-4222-8222-222222222222", tokens_in: 1, tokens_out: 1, finish_reason: "stop" })}\n\n`,
    ].join("");
    await page.route("**/api/chat", (route) =>
      route.request().method() === "POST"
        ? route.fulfill({ status: 200, contentType: "text/event-stream", body: sse })
        : route.fallback(),
    );
    await gotoReady(page, "/chat");
    await page.getByTestId("chat-composer").fill("markdown please");
    await page.getByTestId("chat-send").click();

    const msg = page.getByTestId("chat-assistant-message");
    await expect(msg.getByRole("heading", { name: "Where to look" })).toBeVisible();
    await expect(msg.locator("strong")).toHaveText("Floor 3");
    await expect(msg.locator("li")).toHaveCount(2);
    await expect(msg.locator("code")).toHaveText("A-1");
    await expect(msg.getByRole("table")).toBeVisible();
    await expect(msg.getByRole("cell", { name: "available" })).toBeVisible();
    await expect(msg.getByRole("link", { name: "Open the catalog" })).toHaveAttribute(
      "href",
      "/catalog",
    );
    expect(
      await page.evaluate(() => (window as unknown as { __pwned?: boolean }).__pwned),
    ).toBeUndefined();
    await expect(page.getByTestId("chat-copy")).toBeVisible();
  });
});

test.describe("AI Librarian chat", () => {
  test("streams tokens, tool activity and a citation card (en)", async ({ page }) => {
    await page.route("https://covers.openlibrary.org/**", (route) =>
      route.fulfill({ status: 200, contentType: "image/png", body: PIXEL }),
    );
    await gotoReady(page, "/chat");
    await expect(page.getByRole("heading", { level: 1 })).toHaveText("AI Librarian");
    await expect(page.getByText("How can I help your research?")).toBeVisible();

    const chatRequest = page.waitForRequest(
      (r) => r.url().endsWith("/api/chat") && r.method() === "POST",
    );
    await page.getByTestId("chat-composer").fill("Recommend Neuromancer please");
    await page.getByTestId("chat-send").click();
    expect((await chatRequest).postDataJSON()).toMatchObject({
      message: "Recommend Neuromancer please",
      locale: "en",
    });

    await expect(page.getByTestId("chat-user-message")).toHaveText("Recommend Neuromancer please");
    await expect(page.getByTestId("chat-assistant-message")).toHaveText(
      "From the Athenaeum catalog, I recommend Neuromancer.",
    );
    await expect(page.getByTestId("tool-chip")).toContainText("Searched the catalog");

    const card = page.getByTestId("citation-card").filter({ hasText: "Neuromancer" });
    await expect(card).toBeVisible();
    await expect(card.getByTestId("citation-shelf")).toContainText("Floor 3 • Shelf S-2");
    await expect(card.getByTestId("citation-path")).toHaveText("MAIN/3F/SF/A-1/S-2");
    await expect(card).toHaveAttribute("href", /^\/books\/[0-9a-f-]{36}$/);
    // 2:3 cover thumbnail from the citation's cover_url (docs/06 §6, docs/12 §9).
    const cover = card.getByTestId("citation-cover").getByRole("img", {
      name: "Neuromancer, jacket",
    });
    await expect(cover).toBeVisible();
    await expect(cover).toHaveAttribute("src", COVER_URL);

    // Telemetry meta bar + quota meters (daily bucket from X-RateLimit-* headers).
    await expect(page.getByTestId("chat-model")).not.toHaveText("Model pending");
    await expect(page.getByTestId("chat-tokens")).toBeVisible();
    await expect(page.getByTestId("quota-minute-value")).toHaveText(/^[1-5] \/ 5$/);
    await expect(page.getByTestId("quota-daily-value")).toHaveText(/^\d+ \/ 50$/);
    await expect(page.getByTestId("chat-turn-item")).toHaveAttribute("data-status", "done");

    // The new conversation is URL state and shows up in the drawer; reload replays history.
    await expect(page).toHaveURL(/\/chat\?c=[0-9a-f-]{36}$/);
    const id = new URL(page.url()).searchParams.get("c")!;
    const item = page.getByTestId("conversation-list").locator(`a[href="/chat?c=${id}"]`);
    await expect(item).toHaveText("Recommend Neuromancer please");
    await expect(item).toHaveAttribute("aria-current", "page");
    await gotoReady(page, page.url());
    await expect(page.getByTestId("chat-assistant-message")).toHaveText(
      "From the Athenaeum catalog, I recommend Neuromancer.",
    );

    // Citation cards are persisted on the assistant message and survive the reload.
    const replayed = page.getByTestId("citation-card").filter({ hasText: "Neuromancer" });
    await expect(replayed).toBeVisible();
    await expect(replayed.getByTestId("citation-path")).toHaveText("MAIN/3F/SF/A-1/S-2");
    await expect(
      replayed.getByTestId("citation-cover").getByRole("img", { name: "Neuromancer, jacket" }),
    ).toHaveAttribute("src", COVER_URL);
    const res = await page.request.get(`/api/chat/conversations/${id}`);
    expect(res.status()).toBe(200);
    const detail = (await res.json()) as {
      messages: { role: string; citations?: { title: string; cover_url: string | null }[] }[];
    };
    const assistant = detail.messages.find((m) => m.role === "assistant");
    expect(assistant?.citations?.[0]).toMatchObject({ title: "Neuromancer" });
    expect(assistant?.citations?.[0].cover_url).toMatch(COVER_URL);
  });

  test("replies in Simplified Chinese with a localized citation shelf line (zh-CN)", async ({
    page,
    context,
  }) => {
    await context.addCookies([{ name: "athenaeum_lang", value: "zh-CN", url: WEB_URL }]);
    await gotoReady(page, "/chat");
    await expect(page.getByRole("heading", { level: 1 })).toHaveText("AI 图书管理员");

    const chatRequest = page.waitForRequest(
      (r) => r.url().endsWith("/api/chat") && r.method() === "POST",
    );
    await page.getByTestId("chat-composer").fill("推荐 Neuromancer");
    await page.getByTestId("chat-composer").press("Enter");
    expect((await chatRequest).postDataJSON()).toMatchObject({ locale: "zh-CN" });

    await expect(page.getByTestId("chat-assistant-message")).toHaveText(
      "我在 Athenaeum 馆藏中找到了：Neuromancer。",
    );
    await expect(page.getByTestId("tool-chip")).toContainText("检索了馆藏目录");
    const card = page.getByTestId("citation-card").filter({ hasText: "Neuromancer" });
    await expect(card.getByTestId("citation-shelf")).toContainText("3 楼 • 书架 S-2");
    await expect(card.getByTestId("citation-shelf")).toContainText("科幻与奇幻");
    // Machine path token stays untranslated (docs/14 §6.1).
    await expect(card.getByTestId("citation-path")).toHaveText("MAIN/3F/SF/A-1/S-2");
    await expect(page.getByText("Searched the catalog")).toHaveCount(0);
  });
});
