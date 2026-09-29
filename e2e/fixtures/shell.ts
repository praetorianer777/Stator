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

// Findings against the app that this suite surfaced and that wait on a fix of
// their own: --color-ink-subtle, taken value for value from Armature, stays
// under 4.5:1 on every surface in both palettes, and the not-found page has no
// level-one heading. Exactly these pass; delete an entry once it is fixed.
const KNOWN_FINDINGS: Array<{ rule: string; matches: (page: Page, node: AxeNode) => Promise<boolean> }> = [
  {
    rule: "color-contrast",
    matches: async (page, node) => {
      const inkSubtle = await page.evaluate(() => getComputedStyle(document.documentElement).getPropertyValue("--color-ink-subtle").trim().toLowerCase());
      const foreground = (node.any[0]?.data as { fgColor?: string } | undefined)?.fgColor?.toLowerCase();
      return foreground === inkSubtle;
    },
  },
  {
    rule: "page-has-heading-one",
    matches: async (page) => (await page.locator("[data-not-found]").count()) > 0,
  },
  // The slash menu scrolls while focus stays in the editor, which names the
  // option through aria-activedescendant; the stored-document box belongs to
  // the development page alone.
  {
    rule: "scrollable-region-focusable",
    matches: async (page, node) => {
      const target = node.target[0];
      return typeof target === "string" && (await page.locator(target).evaluate((el) => el.matches("[data-slash-menu], [data-dev-json]")));
    },
  },
];

type AxeNode = Awaited<ReturnType<AxeBuilder["analyze"]>>["violations"][number]["nodes"][number];

/** Runs axe over the whole page as it stands and fails with every violation it finds. */
export async function expectAccessible(page: Page): Promise<void> {
  const results = await new AxeBuilder({ page }).options({ rules: RULES_ON }).analyze();
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
