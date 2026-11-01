import { expect, loginAsDemo, test } from "./fixtures";

test("upload → processing → publish", async ({ page }) => {
  test.setTimeout(90_000);
  await loginAsDemo(page, "/upload");
  await page.waitForURL(/\/upload/);

  await page.locator('input[type="file"]').first().setInputFiles({ name: "clip.mp4", mimeType: "video/mp4", buffer: Buffer.alloc(1024 * 1024) });
  await expect(page.getByRole("progressbar", { name: "Progress" })).toBeVisible();
  await expect(page.getByLabel("Title")).toHaveValue("clip");

  await page.getByLabel(/I confirm that everyone appearing/).check();
  await page.getByRole("button", { name: /Publish/ }).click();
  await expect(page.getByRole("heading", { name: "Your video is published" })).toBeVisible({ timeout: 60_000 });
});

test("rejects unsupported files", async ({ page }) => {
  await loginAsDemo(page, "/upload");
  await page.waitForURL(/\/upload/);
  await page.locator('input[type="file"]').first().setInputFiles({ name: "notes.txt", mimeType: "text/plain", buffer: Buffer.from("hi") });
  await expect(page.getByRole("alert")).toContainText("Unsupported format");
});
