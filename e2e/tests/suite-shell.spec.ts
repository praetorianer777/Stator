import { must } from "../fixtures/api";
import { expect } from "../fixtures/auth";
import { expectAccessible, openAccountMenu, openShell } from "../fixtures/shell";
import { uniqueName } from "../fixtures/seed";
import { createSpace, deleteSpace, uniqueKey } from "../fixtures/spaces";
import { teamTest as test } from "../fixtures/team";

// The shell as the suite's look-and-feel standard draws it (#587): the account
// menu beside the rail, the narrow strip, and the palette's groups.

test.describe("the suite's shell", { tag: ["@auth"] }, () => {
  test("opens the account menu beside the rail, with the person at its head and its groups set apart", { tag: "@desktop" }, async ({ page }) => {
    await openShell(page, "/");
    const trigger = page.locator('[data-rail] [data-action="account"]');
    await expect(trigger).toHaveClass(/\brounded-full\b/);
    const menu = await openAccountMenu(page);
    const rail = (await page.locator("[data-rail]").boundingBox())!;
    const list = (await menu.boundingBox())!;
    expect(list.x).toBeGreaterThanOrEqual(rail.x + rail.width);
    const head = page.locator("[data-account-header]");
    await expect(head.locator("[data-account-header-name]")).not.toBeEmpty();
    await expect(head.locator("[data-account-role]")).toBeVisible();
    await expect(menu.getByRole("separator")).toHaveCount(2);
    await expectAccessible(page);
  });

  test("names the installation on the phone's strip, between the menu button and the bell", { tag: "@mobile" }, async ({ page, api }) => {
    const me = must(await api.GET("/auth/me"));
    await openShell(page, "/");
    const strip = page.locator("[data-nav-strip]");
    // The suite's strip is h-12, the rail's width; the root type is 14px, so that is 42px here as in Armature.
    const { height, rail } = await strip.evaluate((el) => ({
      height: el.getBoundingClientRect().height,
      rail: 3 * Number.parseFloat(getComputedStyle(document.documentElement).fontSize),
    }));
    expect(height).toBe(rail);
    await expect(strip.locator('[data-action="drawer"]')).toBeVisible();
    await expect(strip.locator("[data-installation-name]")).toHaveText(me.organization!.name);
    await expect(strip.locator('[data-action="notifications"]')).toBeVisible();
    await expectAccessible(page);
  });

  test("finds a space and what can be done in the palette, each in a group of its own", async ({ page, api }, testInfo) => {
    const key = uniqueKey(testInfo);
    await createSpace(api, key, uniqueName(testInfo, "Palette"));
    try {
      await openShell(page, "/");
      await page.keyboard.press("Control+k");
      const input = page.locator("[data-quick-search-input]");
      await input.fill(key);
      const spaces = page.getByRole("listbox", { name: "Spaces" });
      await expect(spaces.locator(`[data-quick-search-space="${key}"]`)).toBeVisible();
      await expectAccessible(page);
      await input.press("ArrowDown");
      await input.press("Enter");
      await expect(page).toHaveURL(new RegExp(`/spaces/${key}$`));

      await page.keyboard.press("Control+k");
      await page.locator("[data-quick-search-input]").fill("Create");
      await expect(page.getByRole("listbox", { name: "Actions" }).locator('[data-quick-search-action="new-space"]')).toBeVisible();
    } finally {
      await deleteSpace(api, key);
    }
  });
});
