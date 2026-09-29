import type { Page } from "@playwright/test";
import { expect, test } from "../fixtures/auth";
import { openShell } from "../fixtures/shell";

// The drawer only exists on a narrow screen.
test.describe("drawer motion", { tag: "@mobile" }, () => {
  async function drawerAnimation(page: Page) {
    await openShell(page, "/");
    await page.locator('[data-action="drawer"]').click();
    const drawer = page.locator('[data-sidebar="drawer"]');
    await expect(drawer).toBeVisible();
    return drawer.evaluate((el) => ({ name: getComputedStyle(el).animationName, running: el.getAnimations().length }));
  }

  test("with reduced motion asked for, the drawer appears without animation", async ({ page }) => {
    await page.emulateMedia({ reducedMotion: "reduce" });
    expect(await drawerAnimation(page)).toEqual({ name: "none", running: 0 });
  });

  // The control: without it the check above would pass for a drawer that never animates at all.
  test("without that preference, the drawer slides in", async ({ page }) => {
    await page.emulateMedia({ reducedMotion: "no-preference" });
    expect((await drawerAnimation(page)).name).not.toBe("none");
  });
});
