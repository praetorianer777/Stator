import { expect, test } from "../fixtures/auth";
import { expectAccessible, openShell, startInScheme, type ColourScheme } from "../fixtures/shell";

// Contrast needs real layout and real colours, which is why these run here
// and not in the jsdom suite.
const PATHS = ["/", "/spaces", "/search", "/no-such-page"];
const SCHEMES: ColourScheme[] = ["light", "dark"];

for (const scheme of SCHEMES) {
  for (const path of PATHS) {
    test(`${path} passes axe in ${scheme}`, async ({ page }) => {
      await startInScheme(page, scheme);
      await openShell(page, path);
      await expect(page.locator("html")).toHaveAttribute("data-theme", scheme);
      await expect(page.locator("main").locator("h1, [data-not-found]").first()).toBeVisible();
      await expectAccessible(page);
    });
  }
}
