import type { Page } from "@playwright/test";
import { expect, test } from "../fixtures/auth";
import { expectAccessible, openShell, scrollsSideways } from "../fixtures/shell";

// web/src/config.ts: below SIDEBAR_DRAWER_BELOW_PX the sidebar is a drawer,
// below SIDEBAR_RAIL_BELOW_PX it starts folded to the rail.
const DRAWER_BELOW_PX = 768;
const RAIL_BELOW_PX = 1024;
const HEIGHT_PX = 740;

const NARROW_WIDTHS = [360, 390, DRAWER_BELOW_PX - 1];
const WIDE_WIDTHS = [DRAWER_BELOW_PX, RAIL_BELOW_PX - 1, RAIL_BELOW_PX, 1280];

// Kept clear of the drawer, which leaves a rail's width of the page uncovered.
const BACKDROP_TAP_INSET_PX = 12;

const drawerButton = (page: Page) => page.locator('[data-action="drawer"]');
const drawer = (page: Page) => page.locator('[role="dialog"][data-sidebar="drawer"]');

async function openDrawer(page: Page) {
  await drawerButton(page).click();
  await expect(drawer(page)).toBeVisible();
}

test.describe("narrow screens", { tag: "@mobile" }, () => {
  for (const width of NARROW_WIDTHS) {
    test.describe(`at ${width}px`, () => {
      test.beforeEach(async ({ page }) => {
        await page.setViewportSize({ width, height: HEIGHT_PX });
        await openShell(page, "/");
      });

      test("there is no rail and no sideways scroll, only the drawer button", async ({ page }) => {
        expect(await scrollsSideways(page)).toBe(false);
        await expect(page.locator("[data-rail]")).toHaveCount(0);
        await expect(drawerButton(page)).toBeVisible();
        await expect(drawerButton(page)).toHaveAttribute("aria-expanded", "false");
      });

      test("the drawer opens as a modal dialog and passes axe", async ({ page }) => {
        await openDrawer(page);
        await expect(drawer(page)).toHaveAttribute("aria-modal", "true");
        await expect(drawerButton(page)).toHaveAttribute("aria-expanded", "true");
        expect(await scrollsSideways(page)).toBe(false);
        await expectAccessible(page);
      });

      test("Escape closes the drawer and gives focus back to its button", async ({ page }) => {
        await openDrawer(page);
        await page.keyboard.press("Escape");
        await expect(drawer(page)).toHaveCount(0);
        await expect(drawerButton(page)).toBeFocused();
      });

      test("a tap on the backdrop near the right edge closes the drawer", async ({ page }) => {
        await openDrawer(page);
        await page.touchscreen.tap(width - BACKDROP_TAP_INSET_PX, HEIGHT_PX / 2);
        await expect(drawer(page)).toHaveCount(0);
        await expect(page).toHaveURL(/\/$/);
      });

      test("following a link in the drawer closes it and navigates", async ({ page }) => {
        await openDrawer(page);
        await drawer(page).getByRole("link", { name: "Spaces" }).click();
        await expect(page).toHaveURL(/\/spaces$/);
        await expect(drawer(page)).toHaveCount(0);
        await expect(page.getByRole("heading", { level: 1, name: "Spaces" })).toBeVisible();
      });
    });
  }
});

test.describe("wide screens", { tag: "@desktop" }, () => {
  for (const width of WIDE_WIDTHS) {
    test(`at ${width}px the rail and the sidebar are there and the drawer button is not`, async ({ page }) => {
      await page.setViewportSize({ width, height: HEIGHT_PX });
      await openShell(page, "/");
      await expect(page.locator("[data-rail]")).toBeVisible();
      await expect(drawerButton(page)).toHaveCount(0);
      expect(await scrollsSideways(page)).toBe(false);
      // Below RAIL_BELOW_PX the sidebar starts folded, and the rail's own button unfolds it.
      if (width < RAIL_BELOW_PX) {
        await expect(page.locator('[data-sidebar="open"]')).toHaveCount(0);
        await page.locator('[data-action="sidebar"]').click();
      }
      await expect(page.locator('[data-sidebar="open"]')).toBeVisible();
      await page.locator('[data-sidebar="open"]').getByRole("link", { name: "Spaces" }).click();
      await expect(page).toHaveURL(/\/spaces$/);
    });
  }

  test("resizing across the breakpoint switches between rail and drawer", async ({ page }) => {
    await page.setViewportSize({ width: 1280, height: HEIGHT_PX });
    await openShell(page, "/");
    await expect(page.locator("[data-rail]")).toBeVisible();
    await expect(page.locator('[data-sidebar="open"]')).toBeVisible();

    await page.setViewportSize({ width: DRAWER_BELOW_PX - 1, height: HEIGHT_PX });
    await expect(page.locator("[data-rail]")).toHaveCount(0);
    await expect(page.locator('[data-sidebar="open"]')).toHaveCount(0);
    await openDrawer(page);

    // A drawer left open has no place on a wide screen.
    await page.setViewportSize({ width: DRAWER_BELOW_PX, height: HEIGHT_PX });
    await expect(drawer(page)).toHaveCount(0);
    await expect(drawerButton(page)).toHaveCount(0);
    await expect(page.locator("[data-rail]")).toBeVisible();
  });
});
