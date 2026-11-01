import { expect, loginAsDemo, test } from "./fixtures";

test("protected pages redirect to login and return after signing in", async ({ page }) => {
  await page.goto("/profile");
  await expect(page).toHaveURL(/\/login\?next=%2Fprofile/);
  await loginAsDemo(page, "/profile");
  await expect(page).toHaveURL(/\/profile$/);
  await expect(page.getByText("@demo")).toBeVisible();
});

test("wrong password shows a safe error", async ({ page }) => {
  await page.goto("/login");
  await page.getByLabel("Email or username").fill("demo");
  await page.getByLabel("Password", { exact: true }).fill("wrong-password");
  await page.getByRole("button", { name: "Log in" }).click();
  await expect(page.getByRole("alert")).toContainText("Incorrect email/username or password");
});

test("form validation blocks empty submit", async ({ page }) => {
  await page.goto("/register");
  await page.getByRole("button", { name: "Create account" }).click();
  await expect(page.getByText("Enter a valid email address")).toBeVisible();
  await expect(page.getByText("You must be 18 or older")).toBeVisible();
});

test("logout asks for confirmation", async ({ page }) => {
  await loginAsDemo(page, "/profile");
  await page.getByRole("button", { name: "Log out" }).click();
  const dialog = page.getByRole("dialog", { name: "Log out?" });
  await expect(dialog).toBeVisible();
  await dialog.getByRole("button", { name: "Log out" }).click();
  await expect(page).toHaveURL("/");
});

test("open redirects are ignored", async ({ page }) => {
  await loginAsDemo(page, "//evil.example");
  await expect(page).toHaveURL("/");
});
