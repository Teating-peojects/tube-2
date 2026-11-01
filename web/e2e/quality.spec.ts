import AxeBuilder from "@axe-core/playwright";
import { expect, test } from "./fixtures";

const widths = [320, 360, 375, 390, 430, 768, 1024, 1280, 1440, 1920];

test("no horizontal overflow at any supported width", async ({ page }) => {
  for (const path of ["/", "/trending", "/explore", "/live", "/login"]) {
    for (const width of widths) {
      await page.setViewportSize({ width, height: 800 });
      await page.goto(path);
      await page.waitForLoadState("networkidle");
      const overflow = await page.evaluate(() => document.documentElement.scrollWidth - document.documentElement.clientWidth);
      expect(overflow, `${path} @ ${width}px`).toBeLessThanOrEqual(0);
    }
  }
});

test("skip link is the first focusable element", async ({ page }) => {
  await page.goto("/");
  await page.keyboard.press("Tab");
  await expect(page.getByRole("link", { name: "Skip to content" })).toBeFocused();
});

for (const path of ["/", "/trending", "/login", "/register", "/watch/v1", "/terms"]) {
  test(`axe: no serious accessibility violations on ${path}`, async ({ page }) => {
    await page.goto(path);
    await page.waitForLoadState("networkidle");
    const { violations } = await new AxeBuilder({ page }).withTags(["wcag2a", "wcag2aa"]).analyze();
    expect(violations.filter((v) => v.impact === "serious" || v.impact === "critical"), JSON.stringify(violations.map((v) => v.id))).toEqual([]);
  });
}
