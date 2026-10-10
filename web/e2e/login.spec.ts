import { expect, test } from "@playwright/test";
import { DEMO, DEMO_STATE } from "./accounts";

test.describe("login guard and screen 1 (docs/04 §1, docs/07 §2)", () => {
  test("unauthenticated visitors are redirected to /login with next", async ({ page }) => {
    await page.goto("/catalog");
    await expect(page).toHaveURL(/\/login\?next=%2Fcatalog/);
    await expect(page.getByRole("heading", { level: 1 })).toHaveText("Welcome back");
  });

  test("screen 1 renders role tabs, disabled WeChat, Google SSO and no SAML row", async ({
    page,
  }) => {
    await page.goto("/login");
    for (const name of ["Patron / Student", "Faculty & Scholar", "Staff / Archival"]) {
      await expect(page.getByRole("tab", { name })).toBeVisible();
    }
    await expect(page.getByTestId("wechat-sso")).toBeDisabled();
    await expect(page.getByTestId("google-sso")).toHaveAttribute("href", /^\/api\/auth\/google/);
    await expect(page.getByText(/shibboleth|saml/i)).toHaveCount(0);
  });

  test("invalid credentials show the INVALID_CREDENTIALS message", async ({ page }) => {
    await page.goto("/login");
    await page.getByLabel("Email address").fill(DEMO.email);
    await page.getByLabel("Password", { exact: true }).fill("definitely-wrong-password");
    await page.getByTestId("login-submit").click();
    await expect(page.getByTestId("login-error")).toHaveText("Incorrect email or password.");
    await expect(page).toHaveURL(/\/login/);
  });

  test("demo patron signs in, lands on the catalog shell and signs out", async ({ page }) => {
    await page.goto("/login");
    await page.getByLabel("Email address").fill(DEMO.email);
    await page.getByLabel("Password", { exact: true }).fill(DEMO.password);
    await page.getByTestId("login-submit").click();
    await expect(page).toHaveURL(/\/catalog$/);
    await expect(page.getByRole("link", { name: "My Library" })).toBeVisible();
    await expect(page.getByRole("link", { name: "Staff Admin" })).toHaveCount(0); // patron: no admin link

    await page.getByTestId("sign-out").click();
    await expect(page).toHaveURL(/\/login/);
    await page.goto("/catalog");
    await expect(page).toHaveURL(/\/login\?next=%2Fcatalog/);
  });
});

test.describe("authenticated visitors", () => {
  test.use({ storageState: DEMO_STATE });

  test("/login redirects to /catalog", async ({ page }) => {
    await page.goto("/login");
    await expect(page).toHaveURL(/\/catalog$/);
  });

  test("route smoke: /catalog renders inside the app shell", async ({ page }) => {
    const res = await page.goto("/catalog");
    expect(res?.status()).toBe(200);
    await expect(page.getByRole("navigation", { name: "Primary" })).toBeVisible();
  });
});
