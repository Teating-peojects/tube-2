import { expect, test } from "./fixtures";

test("watch page: player, reactions, comments and login-required modal", async ({ page }) => {
  await page.goto("/watch/v1");
  await expect(page.getByRole("heading", { level: 1 })).toBeVisible();
  await expect(page.getByRole("button", { name: /^Play / })).toBeVisible();

  await page.getByRole("button", { name: "Subscribe" }).click();
  const dialog = page.getByRole("dialog", { name: "Log in required" });
  await expect(dialog).toBeVisible();
  await page.keyboard.press("Escape");
  await expect(dialog).toBeHidden();

  await page.getByRole("heading", { name: /Comments/ }).scrollIntoViewIfNeeded();
  await expect(page.getByRole("list").locator("li").first()).toBeVisible();
});

test("report flow from the video menu", async ({ page }) => {
  await page.goto("/");
  await page.getByRole("button", { name: /^More options for/ }).first().click();
  await page.getByRole("menuitem", { name: "Report" }).click();
  const dialog = page.getByRole("dialog", { name: "Report" });
  await dialog.getByLabel("Spam or scam").check();
  await dialog.getByRole("button", { name: "Submit report" }).click();
  await expect(dialog.getByText("Thanks, your report was submitted")).toBeVisible();
});

test("unknown video shows the unavailable page", async ({ page }) => {
  await page.goto("/watch/does-not-exist");
  await expect(page.getByText("Video unavailable").first()).toBeVisible();
});
