import { expect } from "../fixtures/auth";
import { adminNavigation, expectAccessible, expectNoAdminEntry, openAccountMenu, openAdminPage, openNavigation, openShell } from "../fixtures/shell";
import { teamTest as test } from "../fixtures/team";

// The Admin area of #584: the installation's pages in four sections, reached
// from one Admin entry, refused as a whole to anybody but an administrator,
// with the old /settings addresses leading to it.

const PAGES: Array<{ id: string; section: string; path: RegExp; shows: string }> = [
  { id: "installation", section: "people", path: /\/admin\/installation$/, shows: "[data-installation-page]" },
  { id: "access", section: "people", path: /\/admin\/access\/roles$/, shows: '[data-access-page="roles"]' },
  { id: "users", section: "people", path: /\/admin\/users$/, shows: "[data-users-page]" },
  { id: "site-hub", section: "work", path: /\/admin\/hub$/, shows: "[data-hub-settings]" },
  { id: "templates", section: "work", path: /\/admin\/templates$/, shows: "[data-templates-page]" },
  { id: "stale-pages", section: "work", path: /\/admin\/stale$/, shows: "[data-stale-report]" },
  { id: "example-space-settings", section: "work", path: /\/admin\/example-space$/, shows: "[data-example-space-settings]" },
  { id: "armature-settings", section: "integrations", path: /\/admin\/armature$/, shows: "main h1" },
  { id: "exciter-settings", section: "integrations", path: /\/admin\/exciter$/, shows: "main h1" },
  { id: "connected-apps", section: "integrations", path: /\/admin\/connected-apps$/, shows: "[data-connected-apps-page]" },
  { id: "webhooks", section: "integrations", path: /\/admin\/webhooks$/, shows: "main h1" },
  { id: "audit-log", section: "records", path: /\/admin\/audit$/, shows: "[data-audit-log]" },
  { id: "site-transfer", section: "records", path: /\/admin\/transfer$/, shows: "[data-site-transfer]" },
];

test.describe("the Admin area", { tag: ["@auth"] }, () => {
  test("an administrator reaches it from the avatar menu and every page of it through its navigation", async ({ page }) => {
    await openShell(page, "/spaces");
    await openAdminPage(page, PAGES[0]!.id);
    for (const each of PAGES) {
      await adminNavigation(page, each.id);
      await expect(page).toHaveURL(each.path);
      await expect(page.locator(each.shows).first()).toBeVisible();
    }
  });

  test("its page lists every page in four sections, each with what it is for, and its cards lead there", { tag: ["@desktop"] }, async ({ page }) => {
    await openShell(page, "/spaces");
    await openNavigation(page);
    await page.locator('[data-nav="admin"]').click();
    await expect(page).toHaveURL(/\/admin$/);
    for (const section of ["people", "work", "integrations", "records"]) {
      const ids = PAGES.filter((each) => each.section === section).map((each) => each.id);
      const cards = page.locator(`[data-admin-section="${section}"] [data-admin-card]`);
      await expect(cards).toHaveCount(ids.length);
      expect(await cards.evaluateAll((all) => all.map((card) => card.getAttribute("data-admin-card")))).toEqual(ids);
      for (const card of await cards.all()) await expect(card).toContainText(/\w\.$/);
    }
    await expectAccessible(page);
    for (const each of PAGES) {
      await page.goto("/admin");
      await page.locator(`[data-admin-card="${each.id}"]`).click();
      await expect(page).toHaveURL(each.path);
    }
    await expect(page.locator('[data-admin-nav] [data-nav="site-transfer"]')).toHaveAttribute("data-status", "active");
  });

  test("on a phone its sections are a menu", { tag: ["@mobile"] }, async ({ page }) => {
    await openShell(page, "/admin");
    await expect(page.locator("[data-admin-menu]")).toBeVisible();
    await expect(page.locator("[data-admin-nav]")).toHaveCount(0);
    await page.locator('[data-action="admin-sections"]').click();
    await page.getByRole("menu").locator('[data-nav="webhooks"]').click();
    await expect(page).toHaveURL(/\/admin\/webhooks$/);
    await expect(page.locator('[data-action="admin-sections"]')).toHaveText("Webhooks");
    await expectAccessible(page);
  });

  test("the palette names each page Admin / <page>", async ({ page }) => {
    const palette = page.getByRole("dialog", { name: "Quick search" });
    await openShell(page, "/spaces");
    await page.keyboard.press("Control+k");
    await palette.getByRole("combobox").fill("webh");
    const place = palette.locator('[data-quick-search-place="webhooks"]');
    await expect(place).toContainText("Admin / Webhooks");
    await place.click();
    await expect(page).toHaveURL(/\/admin\/webhooks$/);
  });

  test("somebody who does not administer finds no Admin entry and is refused the whole of it", async ({ pageAs }) => {
    const bob = await pageAs("bob");
    await openShell(bob, "/spaces");
    const menu = await openAccountMenu(bob);
    await expect(menu.locator('[data-action="admin"]')).toHaveCount(0);
    await bob.keyboard.press("Escape");
    for (const path of ["/admin/users", "/admin/connected-apps"]) await expectNoAdminEntry(bob, path);
    await bob.goto("/admin");
    await expect(bob.locator("[data-admin-refused]")).toHaveCount(1);
    await expect(bob.locator("[data-admin-card]")).toHaveCount(0);
  });

  test("an old settings address leads to its page under Admin, keeping its search", async ({ page }) => {
    for (const [from, to] of [
      ["/settings/access/members", /\/admin\/access\/members$/],
      ["/settings/sso", /\/admin\/access\/sso$/],
      ["/settings/permissions", /\/admin\/access\/roles$/],
      ["/settings/brand", /\/admin\/installation$/],
      ["/settings/webhooks", /\/admin\/webhooks$/],
      ["/settings/armature?armature=connected", /\/admin\/armature\?armature=connected$/],
    ] as const) {
      await page.goto(from);
      await expect(page).toHaveURL(to);
    }
    await page.goto("/settings/themes");
    await expect(page).toHaveURL(/\/settings\/themes$/);
  });
});
