import { expect, test } from "@playwright/test";
import { gotoReady } from "./helpers";
import { DEMO_STATE } from "./accounts";

test.describe("language toggle (docs/14 §9, §12)", () => {
  test("toggle switches /login to 中文, sets the cookie and persists", async ({
    page,
    context,
  }) => {
    await page.goto("/login");
    await expect(page.locator("html")).toHaveAttribute("lang", "en");
    await expect(page.getByRole("heading", { level: 1 })).toHaveText("Welcome back");

    await page.getByTestId("lang-zh-CN").click();
    await expect(page.getByRole("heading", { level: 1 })).toHaveText("欢迎回来");
    await expect(page.locator("html")).toHaveAttribute("lang", "zh-CN");
    await expect(page.getByTestId("lang-zh-CN")).toHaveAttribute("aria-pressed", "true");

    const cookie = (await context.cookies()).find((c) => c.name === "athenaeum_lang");
    expect(cookie?.value).toBe("zh-CN");
    expect(cookie?.path).toBe("/");

    await page.reload();
    await expect(page.getByRole("heading", { level: 1 })).toHaveText("欢迎回来");

    // No URL prefix and no literal English leaks on the translated screen.
    expect(new URL(page.url()).pathname).toBe("/login");
    await expect(page.getByText("Sign In to Library")).toHaveCount(0);
    await expect(page.getByText("WeChat Academic ID")).toHaveCount(0);
    await expect(page.getByText("微信学术身份")).toBeVisible();

    await page.getByTestId("lang-en").click();
    await expect(page.getByRole("heading", { level: 1 })).toHaveText("Welcome back");
  });

  test("first visit honours Accept-Language and writes the cookie", async ({ browser }) => {
    const context = await browser.newContext({
      locale: "zh-CN",
      extraHTTPHeaders: { "Accept-Language": "zh-CN,zh;q=0.9" },
    });
    const page = await context.newPage();
    await page.goto("/login");
    await expect(page.getByRole("heading", { level: 1 })).toHaveText("欢迎回来");
    expect((await context.cookies()).find((c) => c.name === "athenaeum_lang")?.value).toBe("zh-CN");
    await context.close();
  });
});

test.describe("language persists across navigation inside the shell", () => {
  test.use({ storageState: DEMO_STATE });

  test("shell nav is localized and the choice survives navigation", async ({ page }) => {
    await gotoReady(page, "/catalog");
    await page.getByTestId("lang-zh-CN").click();
    await expect(page.getByRole("link", { name: "馆藏目录" })).toBeVisible();
    await page.getByRole("link", { name: "我的书架" }).click();
    await expect(page.getByRole("link", { name: "我的书架" })).toHaveAttribute(
      "aria-current",
      "page",
    );
    await expect(page.getByRole("link", { name: "馆藏目录" })).toBeVisible();
    await page.getByTestId("lang-en").click();
    await expect(page.getByRole("link", { name: "Catalog" })).toBeVisible();
  });
});
