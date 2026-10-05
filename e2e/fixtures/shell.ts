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
