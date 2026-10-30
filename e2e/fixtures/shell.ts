import AxeBuilder from "@axe-core/playwright";
import { expect, type Page } from "@playwright/test";

/** The palettes the theme button steps through, as web/src/lib/theme.ts stores them. */
export type ColourScheme = "light" | "dark";
const THEME_STORAGE_KEY = "stator.theme";

/** Makes every page this browser opens start in the given palette, the way a stored choice does. */
export async function startInScheme(page: Page, scheme: ColourScheme): Promise<void> {
  await page.addInitScript(
    ([key, value]) => {
      try {
        localStorage.setItem(key, value);
      } catch {
        // An opaque origin has no storage; the page it opens is not ours.
      }
    },
    [THEME_STORAGE_KEY, scheme] as const,
  );
}

/** Opens a path and waits until the shell has drawn the page. */
export async function openShell(page: Page, path: string): Promise<void> {
  await page.goto(path);
  await expect(page.locator("main#main")).toBeVisible();
}

// color-contrast is what jsdom cannot judge, and region is the rule a portal
// drawn outside every landmark trips; both are named so a change of axe's
// defaults cannot quietly drop them.
const RULES_ON = { "color-contrast": { enabled: true }, region: { enabled: true } };

// Findings this suite surfaced that wait on a fix of their own, each matched
// as narrowly as it can be so nothing else slips through. Empty is the goal;
// add an entry only with its issue, and delete it once that is fixed.
const KNOWN_FINDINGS: Array<{ rule: string; matches: (page: Page, node: AxeNode) => Promise<boolean> }> = [];

type AxeNode = Awaited<ReturnType<AxeBuilder["analyze"]>>["violations"][number]["nodes"][number];

/** Runs axe over the whole page and fails with every violation it finds; a thirdPartyFrames
 *  frame, drawn by another site, is left out with all inside it, so its caller checks its title. */
export async function expectAccessible(page: Page, { thirdPartyFrames = [] }: { thirdPartyFrames?: string[] } = {}): Promise<void> {
  let builder = new AxeBuilder({ page }).options({ rules: RULES_ON });
  for (const frame of thirdPartyFrames) builder = builder.exclude(frame);
  const results = await builder.analyze();
  const found: string[] = [];
  for (const violation of results.violations) {
    for (const node of violation.nodes) {
      const known = KNOWN_FINDINGS.find((each) => each.rule === violation.id);
      if (known && (await known.matches(page, node))) continue;
      found.push(`${violation.id} (${violation.impact}) at ${node.target.join(" ")}: ${node.failureSummary ?? violation.help}`);
    }
  }
  expect(found, "axe found accessibility violations").toEqual([]);
}

/** Whether the page scrolls sideways, which a narrow screen must never do. */
export async function scrollsSideways(page: Page): Promise<boolean> {
  return page.evaluate(() => document.documentElement.scrollWidth > window.innerWidth);
}

// web/src/config.ts: below SIDEBAR_DRAWER_BELOW_PX the rail and the sidebar
// give way to a drawer, whose foot holds the avatar.
const DRAWER_BELOW_PX = 768;

function narrow(page: Page): boolean {
  return (page.viewportSize()?.width ?? DRAWER_BELOW_PX) < DRAWER_BELOW_PX;
}

/** Opens the drawer on a phone, where the rail's and the sidebar's entries live, and says whether it did; on a wide screen they are always there. */
export async function openNavigation(page: Page): Promise<boolean> {
  if (!narrow(page)) return false;
  const drawer = page.locator('[role="dialog"][data-sidebar="drawer"]');
  if (!(await drawer.isVisible())) await page.locator('[data-action="drawer"]').click();
  await expect(drawer).toBeVisible();
  return true;
}

/** Opens the avatar's menu, from the rail or from the drawer's foot, and answers it. */
export async function openAccountMenu(page: Page) {
  await expect(page.locator("main#main")).toBeVisible();
  await openNavigation(page);
  await page.locator('[data-action="account"]').click();
  const menu = page.getByRole("menu", { name: "Your account" });
  await expect(menu).toBeVisible();
  return menu;
}

/** The settings area's own sidebar, or the drawer that holds it on a phone. */
export function settingsSidebar(page: Page) {
  return page.locator('[data-settings-sidebar], [role="dialog"][data-sidebar="drawer"]');
}

/** Goes to a page of the settings area as a site administrator does: Settings in the avatar menu, then the page by its data-nav. */
export async function openSettingsPage(page: Page, id: string): Promise<void> {
  const menu = await openAccountMenu(page);
  await menu.locator('[data-action="settings"]').click();
  await expect(page.locator("[data-settings-home]")).toBeVisible();
  await openNavigation(page);
  await settingsSidebar(page).locator(`[data-nav="${id}"]`).click();
}

/** Checks a person who is no site administrator is offered neither Settings in the avatar menu nor the page in the settings sidebar. */
export async function expectNoSettingsEntry(page: Page, id: string): Promise<void> {
  const menu = await openAccountMenu(page);
  await expect(menu.locator('[data-action="profile"]')).toBeVisible();
  await expect(menu.locator('[data-action="settings"]')).toHaveCount(0);
  await expect(menu.locator(`[data-action="${id}"]`)).toHaveCount(0);
  await page.keyboard.press("Escape");
  await expect(menu).toHaveCount(0);
  await page.goto("/settings/profile");
  await openNavigation(page);
  const sidebar = settingsSidebar(page);
  await expect(sidebar.locator('[data-nav="profile"]')).toBeVisible();
  await expect(sidebar.locator(`[data-nav="${id}"]`)).toHaveCount(0);
}

/** Presses the danger button of the dialog that asks before something goes, and waits for it to close. */
export async function confirm(page: Page): Promise<void> {
  const dialog = page.locator('[role="dialog"][data-confirm]');
  await expect(dialog).toBeVisible();
  await dialog.locator('[data-action="confirm"]').click();
  await expect(dialog).toHaveCount(0);
}

/** The latest toast that says a sentence, among those in the corner. */
export function toast(page: Page, text: string | RegExp) {
  return page.locator("[data-toast]").filter({ hasText: text }).last();
}
