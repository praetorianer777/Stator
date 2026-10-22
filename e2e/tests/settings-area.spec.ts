import type { Page } from "@playwright/test";
import { expect } from "../fixtures/auth";
import { expectAccessible, openAccountMenu, openNavigation, openShell, settingsSidebar } from "../fixtures/shell";
import { uniqueName } from "../fixtures/seed";
import { createSpace, deleteSpace, uniqueKey } from "../fixtures/spaces";
import { createPerson, removePerson, teamTest as test } from "../fixtures/team";

// The menus of #464: the avatar menu holds the person's own pages, the other
// tools, Settings for site administrators and Sign out; the settings area at
// /settings has a sidebar of its own; the rail and the sidebar hold the work.

const OWN = ["profile", "notification-settings", "watching", "themes", "tokens"];
const SITE = [
  "sso-settings",
  "site-permissions",
  "site-hub",
  "templates",
  "stale-pages",
  "example-space-settings",
  "site-brand",
  "site-transfer",
  "armature-settings",
  "exciter-settings",
  "webhooks",
  "audit-log",
];

/** The avatar menu's own entries by data-action, leaving out the tools of "Switch to", which another spec may have connected. */
async function accountEntries(page: Page): Promise<string[]> {
  const menu = await openAccountMenu(page);
  const actions = await menu.locator("[role=menuitem][data-action]").evaluateAll((items) => items.map((item) => item.getAttribute("data-action") ?? ""));
  await page.keyboard.press("Escape");
  return actions;
}

async function settingsEntries(page: Page, group: "settings-you" | "settings-site"): Promise<string[]> {
  return settingsSidebar(page)
    .locator(`[data-sidebar-group="${group}"] [data-nav]`)
    .evaluateAll((links) => links.map((link) => link.getAttribute("data-nav") ?? ""));
}

test.describe("the avatar menu and the settings area", { tag: ["@auth"] }, () => {
  test("a site administrator reaches every page of the organization from Settings", async ({ page }) => {
    await openShell(page, "/spaces");
    expect(await accountEntries(page)).toEqual([...OWN, "settings", "sign-out"]);

    const menu = await openAccountMenu(page);
    await menu.locator('[data-action="settings"]').click();
    await expect(page).toHaveURL(/\/settings$/);
    await expect(page.locator('[data-settings-card="you"]')).toBeVisible();
    await expect(page.locator('[data-settings-card="site"] [data-settings-link]')).toHaveCount(SITE.length);
    await expectAccessible(page);

    await openNavigation(page);
    expect(await settingsEntries(page, "settings-you")).toEqual([...OWN, "armature-account", "exciter-account"]);
    expect(await settingsEntries(page, "settings-site")).toEqual(SITE);
    await settingsSidebar(page).locator('[data-nav="webhooks"]').click();
    await expect(page).toHaveURL(/\/settings\/webhooks$/);
    await page.locator("main").getByRole("navigation", { name: "Breadcrumb" }).getByRole("link", { name: "Settings" }).click();
    await expect(page).toHaveURL(/\/settings$/);

    await openNavigation(page);
    await settingsSidebar(page).locator('[data-action="leave-settings"]').click();
    await expect(page).toHaveURL(/\/spaces$/);
    await openNavigation(page);
    await expect(page.locator('[data-sidebar-group="wiki"]')).toBeVisible();
  });

  test("a member has their own pages and no Settings", async ({ pageAs }) => {
    const bob = await pageAs("bob");
    await openShell(bob, "/");
    expect(await accountEntries(bob)).toEqual([...OWN, "sign-out"]);

    await openShell(bob, "/settings");
    await expect(bob.locator('[data-settings-card="you"]')).toBeVisible();
    await expect(bob.locator('[data-settings-card="site"]')).toHaveCount(0);
    await openNavigation(bob);
    expect(await settingsEntries(bob, "settings-you")).toEqual([...OWN, "armature-account", "exciter-account"]);
    await expect(settingsSidebar(bob).locator('[data-sidebar-group="settings-site"]')).toHaveCount(0);
  });

  test("a guest has no Access tokens, no Settings, no Hub and no Spaces", async ({ pageAs, api }, testInfo) => {
    const key = uniqueKey(testInfo);
    await createSpace(api, key, uniqueName(testInfo, "Visits"));
    const gina = await createPerson(`gina-${key}`, `Gina ${key}`, "guest", key);
    try {
      const guest = await pageAs(gina);
      await openShell(guest, `/s/${key}`);
      expect(await accountEntries(guest)).toEqual(["profile", "notification-settings", "watching", "themes", "sign-out"]);
      await openNavigation(guest);
      for (const absent of ["hub", "spaces"]) await expect(guest.locator(`[data-nav="${absent}"]`)).toHaveCount(0);
      await expect(guest.locator('[data-sidebar-group="wiki"] [data-nav="search"]')).toBeVisible();

      await openShell(guest, "/settings");
      await expect(guest.locator('[data-settings-card="site"]')).toHaveCount(0);
      await expect(guest.locator('[data-settings-card="you"] [data-settings-link="tokens"]')).toHaveCount(0);
    } finally {
      await removePerson(gina.id);
      await deleteSpace(api, key);
    }
  });
});

test.describe("the drawer at phone width", { tag: ["@auth", "@mobile"] }, () => {
  test("holds the wiki's groups and the avatar, and the settings sidebar in the settings area", async ({ page }) => {
    await openShell(page, "/");
    await expect(page.locator("[data-rail]")).toHaveCount(0);
    await expect(page.locator("[data-nav-strip]")).toBeVisible();
    await openNavigation(page);
    const drawer = page.locator('[role="dialog"][data-sidebar="drawer"]');
    await expect(drawer.locator('[data-sidebar-group="wiki"] [data-nav="reviews"]')).toBeVisible();
    await expect(drawer.locator('[data-drawer-foot] [data-action="account"]')).toBeVisible();
    await expect(drawer.locator('[data-drawer-foot] [data-action="notifications"]')).toBeVisible();
    await expectAccessible(page);

    await drawer.locator('[data-drawer-foot] [data-action="account"]').click();
    await page.getByRole("menu", { name: "Your account" }).locator('[data-action="settings"]').click();
    await expect(page).toHaveURL(/\/settings$/);
    await openNavigation(page);
    await expect(drawer.locator('[data-action="leave-settings"]')).toBeVisible();
    await expect(drawer.locator('[data-nav="audit-log"]')).toBeVisible();
    await expect(drawer.locator('[data-sidebar-group="wiki"]')).toHaveCount(0);
    await expectAccessible(page);
  });
});
