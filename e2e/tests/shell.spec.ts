import { expect, test } from "../fixtures/auth";
import { openShell } from "../fixtures/shell";

// The shell around every page: a visitor's until sign-in exists, alice's after.

const PAGES = [
  { path: "/", heading: "Home" },
  { path: "/spaces", heading: "Spaces" },
  { path: "/search", heading: "Search" },
];

for (const { path, heading } of PAGES) {
  test(`${path} draws the shell around its page`, async ({ page }) => {
    await openShell(page, path);
    await expect(page.locator("[data-top-bar]")).toBeVisible();
    await expect(page.getByRole("heading", { level: 1, name: heading })).toBeVisible();
    await expect(page).toHaveTitle(/Stator/);
  });
}

test("the search box and Ctrl K both open quick search", async ({ page }) => {
  const palette = page.getByRole("dialog", { name: "Quick search" });
  await openShell(page, "/spaces");
  await page.locator('[data-action="search"]').click();
  await expect(palette).toBeVisible();
  await page.keyboard.press("Escape");
  await expect(palette).toHaveCount(0);

  await openShell(page, "/");
  await page.keyboard.press("Control+k");
  await expect(palette.getByRole("combobox")).toBeFocused();
});

test("an unknown path shows the not-found page inside the shell, with a way home", async ({ page }) => {
  await openShell(page, "/no-such-page");
  const notFound = page.locator("[data-not-found]");
  await expect(page.getByRole("heading", { level: 1, name: "Page not found" })).toBeVisible();
  await expect(notFound).toBeVisible();
  await expect(page.locator("[data-top-bar]")).toBeVisible();

  await notFound.locator('[data-action="home"]').click();
  await expect(page).toHaveURL(/\/$/);
  await expect(page.getByRole("heading", { level: 1, name: "Home" })).toBeVisible();
});
