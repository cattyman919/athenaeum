import type { Page } from "@playwright/test";

/**
 * Navigates and waits until React has hydrated and every streamed Suspense boundary has
 * been swapped into place (Next parks pending content in hidden `div#S:n` templates, which
 * would otherwise make test ids resolve twice).
 */
export async function gotoReady(page: Page, url: string) {
  const res = await page.goto(url);
  await page.locator("html[data-hydrated='true']").waitFor({ state: "attached" });
  await page.waitForFunction(() => !document.querySelector("div[hidden][id^='S:']"));
  return res;
}

/**
 * Switches language through the header toggle. The toggle writes the cookie and hard-reloads
 * (docs/14 §9), so wait for the reloaded page to hydrate and finish streaming.
 */
export async function switchLang(page: Page, locale: "en" | "zh-CN") {
  const loaded = page.waitForEvent("load");
  await page.getByTestId(`lang-${locale}`).click();
  await loaded;
  await page.locator("html[data-hydrated='true']").waitFor({ state: "attached" });
  await page.waitForFunction(() => !document.querySelector("div[hidden][id^='S:']"));
}
