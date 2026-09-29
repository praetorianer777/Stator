import { expect, test } from "../fixtures/auth";
import { expectAccessible, openShell } from "../fixtures/shell";

test("the first Tab shows the skip link, and Enter moves focus to the content", async ({ page }) => {
  await openShell(page, "/");
  await page.keyboard.press("Tab");

  const skip = page.locator("[data-skip-link]");
  await expect(skip).toBeFocused();
  await expect(skip).toBeVisible();
  await expect(skip).toHaveText("Skip to content");
  // The ring is what tells a keyboard user where they are; sr-only would
  // leave the link focused but one pixel wide.
  expect(await skip.evaluate((el) => el.matches(":focus-visible"))).toBe(true);
  const ring = await skip.evaluate((el) => {
    const style = getComputedStyle(el);
    return { style: style.outlineStyle, width: Number.parseFloat(style.outlineWidth), box: el.getBoundingClientRect().width };
  });
  expect(ring.style).not.toBe("none");
  expect(ring.width).toBeGreaterThan(0);
  expect(ring.box).toBeGreaterThan(1);

  await page.keyboard.press("Enter");
  await expect(page.locator("main#main")).toBeFocused();
  await expect(page).toHaveURL(/\/$/);
});

test("the account menu works from the keyboard and passes axe while open", async ({ page }) => {
  await openShell(page, "/");
  const trigger = page.locator('[data-action="account"]');
  await trigger.focus();
  await page.keyboard.press("Enter");

  const menu = page.getByRole("menu", { name: "Your account" });
  await expect(menu).toBeVisible();
  await expect(trigger).toHaveAttribute("aria-expanded", "true");
  const items = menu.getByRole("menuitem");
  await expect(items.first()).toBeFocused();

  await page.keyboard.press("ArrowDown");
  await expect(menu.locator('[data-action="themes"]')).toBeFocused();
  await page.keyboard.press("End");
  await expect(items.last()).toBeFocused();
  await page.keyboard.press("Home");
  await expect(items.first()).toBeFocused();

  await expectAccessible(page);

  await page.keyboard.press("Escape");
  await expect(menu).toHaveCount(0);
  await expect(trigger).toBeFocused();
  await expect(trigger).toHaveAttribute("aria-expanded", "false");
});
