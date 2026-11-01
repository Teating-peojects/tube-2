import { base as test, expect } from "./fixtures";

test("visitors are sent to the 18+ gate and can continue", async ({ page }) => {
  await page.goto("/");
  await expect(page).toHaveURL(/\/age-verification/);
  await expect(page.getByRole("heading", { name: "Age Restricted Content" })).toBeVisible();
  await page.getByRole("button", { name: "I am 18+" }).click();
  await expect(page).toHaveURL("/");
  await expect(page.getByRole("combobox", { name: /search/i }).or(page.getByRole("link", { name: "Search" })).first()).toBeVisible();
});

test("legal pages are reachable without confirming age", async ({ page }) => {
  for (const path of ["/terms", "/privacy", "/community-guidelines", "/dmca"]) {
    await page.goto(path);
    await expect(page).toHaveURL(path);
    await expect(page.getByRole("heading", { level: 1 })).toBeVisible();
  }
});
