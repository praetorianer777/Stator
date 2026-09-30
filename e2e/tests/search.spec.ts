import type { Locator, Page, TestInfo } from "@playwright/test";
import { expect } from "../fixtures/auth";
import { orgTest as test } from "../fixtures/org";
import type { StatorApi } from "../fixtures/api";
import { uniqueName } from "../fixtures/seed";
import { expectAccessible, startInScheme, type ColourScheme } from "../fixtures/shell";
import { createPage, createSpace, deleteSpace, uniqueKey, type Space } from "../fixtures/spaces";

const hit = (page: Page, title: string) => page.locator(`[data-search-hit="${title}"]`);
const palette = (page: Page) => page.getByRole("dialog", { name: "Quick search" });
const option = (page: Page, title: string) => palette(page).locator(`[data-quick-search-option="${title}"]`);

// A script's writes can reach the index a moment after they answer, and the
// browser reads from a replica of its own, so a search is asked again until
// what it should find is there.
async function searchUntil(page: Page, path: string, found: (page: Page) => Locator) {
  await expect(async () => {
    await page.goto(path);
    await expect(found(page)).toBeVisible({ timeout: 1_000 });
  }).toPass();
}

test.describe("search", { tag: ["@auth"] }, () => {
  const made: string[] = [];
  test.afterEach(async ({ api }) => {
    for (const key of made.splice(0)) await deleteSpace(api, key);
  });

  async function freshSpace(api: StatorApi, testInfo: TestInfo, label: string, suffix = ""): Promise<Space> {
    const key = uniqueKey(testInfo).slice(0, 10 - suffix.length) + suffix;
    made.push(key);
    return createSpace(api, key, uniqueName(testInfo, label));
  }

  test("finds a page by its title and by words of its body, with the matches marked", async ({ page, api }, testInfo) => {
    const space = await freshSpace(api, testInfo, "Find");
    await createPage(api, space.homePageId, "Zephyr quarterly report", "The flamingo budget grew this quarter.");

    await searchUntil(page, "/search?q=zephyr", (p) => hit(p, "Zephyr quarterly report"));
    await expect(hit(page, "Zephyr quarterly report").locator("mark").first()).toHaveText("Zephyr");

    await page.locator("[data-search-query]").fill("flamingo");
    await page.locator("[data-search-query]").press("Enter");
    await expect(page).toHaveURL(/\/search\?q=flamingo$/);
    const found = hit(page, "Zephyr quarterly report");
    await expect(found.locator("[data-search-snippet] mark")).toHaveText("flamingo");
    await found.getByRole("link", { name: "Zephyr quarterly report" }).click();
    await expect(page.locator("main").getByRole("heading", { level: 1 })).toHaveText("Zephyr quarterly report");
  });

  test("narrows the hits to a space, and keeps the filter in the address", async ({ page, api }, testInfo) => {
    const here = await freshSpace(api, testInfo, "Here", "H");
    const there = await freshSpace(api, testInfo, "There", "T");
    await createPage(api, here.homePageId, "Nebula notes here");
    await createPage(api, there.homePageId, "Nebula notes there");

    await searchUntil(page, "/search?q=nebula", (p) => hit(p, "Nebula notes there"));
    await expect(hit(page, "Nebula notes here")).toBeVisible();
    await page.locator('[data-filter="space"]').selectOption(here.key);
    await expect(page).toHaveURL(new RegExp(`space=${here.key}`));
    await expect(hit(page, "Nebula notes here")).toBeVisible();
    await expect(hit(page, "Nebula notes there")).toHaveCount(0);

    await page.reload();
    await expect(page.locator('[data-filter="space"]')).toHaveValue(here.key);
    await expect(hit(page, "Nebula notes there")).toHaveCount(0);
  });

  test("quick search opens a page from the keyboard, and Enter on the words opens the full search", async ({ page, api }, testInfo) => {
    const space = await freshSpace(api, testInfo, "Quick");
    const target = await createPage(api, space.homePageId, "Kestrel runbook");
    await createPage(api, space.homePageId, "Kestrel rota");

    await page.goto("/");
    await expect(async () => {
      await page.keyboard.press("Control+k");
      await palette(page).getByRole("combobox").fill("kestrel ru");
      await expect(option(page, "Kestrel runbook")).toBeVisible({ timeout: 1_000 });
    }).toPass();
    const input = palette(page).getByRole("combobox");
    await expect(input).toBeFocused();
    await page.keyboard.press("ArrowDown");
    await expect(input).toHaveAttribute("aria-activedescendant", (await option(page, "Kestrel runbook").getAttribute("id")) ?? "");
    await expect(option(page, "Kestrel runbook")).toHaveAttribute("aria-selected", "true");
    await page.keyboard.press("Enter");
    await expect(page).toHaveURL(new RegExp(`/p/${target.id}/kestrel-runbook$`));
    await expect(palette(page)).toHaveCount(0);

    await page.keyboard.press("Control+k");
    await palette(page).getByRole("combobox").fill("kestrel");
    await page.keyboard.press("Enter");
    await expect(page).toHaveURL(/\/search\?q=kestrel$/);
    await expect(page.locator("[data-search-query]")).toHaveValue("kestrel");
  });

  test("quick search lists the pages opened last, the latest first", async ({ page, api }, testInfo) => {
    const space = await freshSpace(api, testInfo, "Recent");
    const first = await createPage(api, space.homePageId, "Heron minutes");
    const second = await createPage(api, space.homePageId, "Osprey minutes");

    for (const each of [first, second]) {
      await page.goto(`/s/${space.key}/p/${each.id}/${each.title.toLowerCase().replace(" ", "-")}`);
      await expect(page.locator("main").getByRole("heading", { level: 1 })).toHaveText(each.title);
    }
    await expect(async () => {
      await page.goto("/");
      await page.keyboard.press("Control+k");
      const recent = palette(page).locator('[data-quick-search-list="recent"] [role="option"]');
      await expect(recent.nth(1)).toBeVisible({ timeout: 1_000 });
      expect((await recent.allTextContents()).slice(0, 2).map((text) => text.split(space.name)[0])).toEqual(["Osprey minutes", "Heron minutes"]);
    }).toPass();
  });

  test("a page in the trash is not found", async ({ page, api }, testInfo) => {
    const space = await freshSpace(api, testInfo, "Trashed");
    await createPage(api, space.homePageId, "Walrus handover kept");
    const gone = await createPage(api, space.homePageId, "Walrus handover gone");
    await api.DELETE("/pages/{pageID}", { params: { path: { pageID: gone.id } } });

    await searchUntil(page, "/search?q=walrus", (p) => hit(p, "Walrus handover kept"));
    await expect(hit(page, "Walrus handover gone")).toHaveCount(0);

    await page.keyboard.press("Control+k");
    await palette(page).getByRole("combobox").fill("walrus handover");
    await expect(option(page, "Walrus handover kept")).toBeVisible();
    await expect(option(page, "Walrus handover gone")).toHaveCount(0);
  });

  for (const scheme of ["light", "dark"] as ColourScheme[]) {
    test(`the search page and quick search pass axe in ${scheme}`, async ({ page, api }, testInfo) => {
      const space = await freshSpace(api, testInfo, "Axe");
      await createPage(api, space.homePageId, "Puffin guide", "Where the puffin nests.");
      await startInScheme(page, scheme);

      await searchUntil(page, "/search?q=puffin", (p) => hit(p, "Puffin guide"));
      await expect(page.locator("html")).toHaveAttribute("data-theme", scheme);
      await expectAccessible(page);

      await page.keyboard.press("Control+k");
      await palette(page).getByRole("combobox").fill("puffin");
      await expect(option(page, "Puffin guide")).toBeVisible();
      await page.keyboard.press("ArrowDown");
      await expectAccessible(page);
    });
  }
});
