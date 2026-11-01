import { expect, loginAsDemo, test } from "./fixtures";

test("watch later, playlists and notifications", async ({ page }) => {
  await loginAsDemo(page, "/watch-later");
  await expect(page.getByRole("heading", { name: "Watch later" })).toBeVisible();
  await expect(page.locator("article").first()).toBeVisible();

  await page.goto("/playlists");
  await expect(page.getByRole("heading", { name: "Playlists" })).toBeVisible();
  await page.getByRole("button", { name: "New playlist" }).click();
  await page.getByLabel("Name").fill("E2E list");
  await page.getByRole("button", { name: "Create" }).click();
  await expect(page.getByText("E2E list")).toBeVisible();

  await page.goto("/notifications");
  await page.getByRole("button", { name: "Mark all as read" }).click();
  await page.getByRole("tab", { name: "Unread" }).click();
  await expect(page.getByText("You’re all caught up")).toBeVisible();
});
