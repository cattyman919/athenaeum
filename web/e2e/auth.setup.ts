import { expect, test as setup } from "@playwright/test";
import { ADMIN, ADMIN_STATE, DEMO, DEMO_STATE } from "./accounts";

// Captures a session once per role into Playwright storageState (docs/09 §8.1).
// Uses the API directly (2 login-bucket hits total) so UI specs keep their budget.
for (const [label, account, path] of [
  ["admin", ADMIN, ADMIN_STATE],
  ["demo patron", DEMO, DEMO_STATE],
] as const) {
  setup(`authenticate ${label}`, async ({ request }) => {
    const res = await request.post("/api/auth/login", {
      data: { email: account.email, password: account.password },
    });
    expect(res.status(), await res.text()).toBe(200);
    await request.storageState({ path });
  });
}
