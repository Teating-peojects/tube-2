import { expect, test } from "./fixtures";

test("home lists videos, filters by category and shows the right navigation", async ({ page, viewport }) => {
  await page.goto("/");
  await expect(page.locator("article").first()).toBeVisible();
  await page.getByRole("button", { name: "Music" }).click();
  await expect(page.getByRole("button", { name: "Music" })).toHaveAttribute("aria-pressed", "true");
  const wide = (viewport?.width ?? 1280) >= 1024;
  await expect(page.getByRole("navigation", { name: wide ? "Main" : "Primary" })).toBeVisible();
});

test("search with filters", async ({ page, isMobile }) => {
  await page.goto(isMobile ? "/search" : "/");
  const box = page.getByRole("combobox", { name: "Search REDXTUBE" }).first();
  await box.fill("neon");
  await box.press("Enter");
  await expect(page).toHaveURL(/\/search\?q=neon/);
  await expect(page.getByRole("status").filter({ hasText: /results? for/ })).toBeVisible();
  await page.getByLabel("Sort").selectOption("latest");
  await expect(page).toHaveURL(/sort=latest/);
  await page.getByRole("button", { name: "Channels" }).click();
  await expect(page).toHaveURL(/type=channels/);
});

test("trending tabs work with the keyboard", async ({ page }) => {
  await page.goto("/trending");
  const tab = page.getByRole("tab", { name: "Trending Now" });
  await tab.focus();
  await page.keyboard.press("ArrowRight");
  await expect(page.getByRole("tab", { name: "Most Viewed" })).toHaveAttribute("aria-selected", "true");
  await expect(page.getByRole("list").filter({ has: page.locator("article") }).first()).toBeVisible();
});
