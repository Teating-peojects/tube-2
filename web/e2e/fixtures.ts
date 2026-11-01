import { expect, test as base, type Page } from "@playwright/test";

/** Same as Playwright's `test`, but the visitor has already passed the 18+ gate. */
export const test = base.extend({
  context: async ({ context, baseURL }, use) => {
    await context.addCookies([{ name: "xh_age_ok", value: "1", url: baseURL! }]);
    await use(context);
  },
});
export { expect, base };

export async function loginAsDemo(page: Page, next = "/") {
  await page.goto(`/login?next=${encodeURIComponent(next)}`);
  await page.getByLabel("Email or username").fill("demo");
  await page.getByLabel("Password", { exact: true }).fill("Demo-12345");
  await page.getByRole("button", { name: "Log in" }).click();
}
