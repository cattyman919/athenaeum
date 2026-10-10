import { expect, test, type Page } from "@playwright/test";
import { ADMIN_STATE, DEMO_STATE } from "./accounts";
import { gotoReady } from "./helpers";

// Loading / empty / error states (docs/07 §9) and their zh-CN copy (docs/14).

const WEB_URL = "http://localhost:13000";
const MISSING_ID = "00000000-0000-0000-0000-000000000000";

async function useZh(page: Page) {
  await page.context().addCookies([{ name: "athenaeum_lang", value: "zh-CN", url: WEB_URL }]);
}

test.describe("patron states", () => {
  test.use({ storageState: DEMO_STATE });

  test("unknown conversation id shows the inline load error and the empty-chat prompts", async ({
    page,
  }) => {
    await gotoReady(page, `/chat?c=${MISSING_ID}`);
    await expect(page.getByTestId("chat-load-error")).toHaveText(
      "We couldn't load this conversation.",
    );
    // Thread stays usable: the empty-history prompt chips are offered (docs/07 §9).
    await expect(page.getByTestId("suggestion-babylonian")).toBeVisible();
    await expect(page.getByTestId("chat-composer")).toBeEditable();
    await expect(page.getByTestId("chat-skeleton")).toHaveCount(0);
  });

  test("malformed conversation id shows the load error in zh-CN", async ({ page }) => {
    await useZh(page);
    await gotoReady(page, "/chat?c=not-a-uuid");
    await expect(page.getByTestId("chat-load-error")).toHaveText("无法加载此对话。");
  });

  test("unknown route renders the localized not-found page with a 404", async ({ page }) => {
    const res = await page.goto("/no-such-shelf");
    expect(res?.status()).toBe(404);
    await expect(page.getByTestId("route-not-found")).toContainText("Page not found");
    await page.getByRole("link", { name: "Back to the catalog" }).click();
    await expect(page).toHaveURL(/\/catalog$/);

    await useZh(page);
    await page.goto("/no-such-shelf");
    await expect(page.getByTestId("route-not-found")).toContainText("页面不存在");
  });

  test("offline banner follows navigator.onLine", async ({ page, context }) => {
    await gotoReady(page, "/catalog");
    await expect(page.getByTestId("offline-banner")).toHaveCount(0);
    await context.setOffline(true);
    await expect(page.getByTestId("offline-banner")).toContainText("You are offline");
    await context.setOffline(false);
    await expect(page.getByTestId("offline-banner")).toHaveCount(0);
  });

  test("catalog section facets and empty-search chips read in zh-CN", async ({ page }) => {
    await useZh(page);
    await gotoReady(page, "/catalog");
    const sidebar = page.getByTestId("facet-sidebar");
    await expect(sidebar).toContainText("科幻与奇幻");
    await expect(sidebar).toContainText("科学、数学与技术");
    await expect(sidebar).not.toContainText("Science Fiction & Speculative");

    await gotoReady(page, "/catalog?q=zzzzqqqq");
    const empty = page.getByTestId("catalog-empty");
    await expect(empty).toBeVisible();
    await expect(empty.getByRole("link", { name: "期刊与档案" })).toHaveAttribute(
      "href",
      "/catalog?section=PER",
    );
    await expect(empty).not.toContainText("Periodicals & Archives");
  });

  test("/library shelf lines use the localized wayfinding template", async ({ page }) => {
    await gotoReady(page, "/library?stage=reading");
    await expect(page.locator("[data-title='Dune']").getByTestId("row-shelf")).toHaveText(
      "Floor 3 • Shelf S-2",
    );
    await useZh(page);
    await gotoReady(page, "/library?stage=reading");
    await expect(page.locator("[data-title='Dune']").getByTestId("row-shelf")).toHaveText(
      "3 楼 • 书架 S-2",
    );
    await expect(page.getByTestId("library-skeleton")).toHaveCount(0);
  });
});

test.describe("empty library tab", () => {
  // The admin account never marks a book "read" (library-actions only uses reading/want_to_read).
  test.use({ storageState: ADMIN_STATE });

  test("empty stage shows the CTA card to /catalog (en + zh-CN)", async ({ page }) => {
    await gotoReady(page, "/library?stage=read");
    const empty = page.getByTestId("library-empty");
    await expect(empty).toContainText("Nothing here yet");
    await expect(empty.getByRole("link", { name: "Browse the catalog" })).toHaveAttribute(
      "href",
      "/catalog",
    );

    await useZh(page);
    await gotoReady(page, "/library?stage=read");
    await expect(page.getByTestId("library-empty")).toContainText("这里还没有内容");
    await page.getByTestId("library-empty").getByRole("link", { name: "浏览馆藏目录" }).click();
    await expect(page).toHaveURL(/\/catalog$/);
  });
});
