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
