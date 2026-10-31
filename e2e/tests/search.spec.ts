import type { Page, TestInfo } from "@playwright/test";
import { expect } from "../fixtures/auth";
import { teamTest as test } from "../fixtures/team";
import type { StatorApi } from "../fixtures/api";
import { ONE_LOOK, openShowing, openUntil } from "../fixtures/replica";
import { uniqueName, uniqueWord } from "../fixtures/seed";
import { expectAccessible, startInScheme, type ColourScheme } from "../fixtures/shell";
import { createPage, createSpace, deleteSpace, uniqueKey, type Space } from "../fixtures/spaces";

const hit = (page: Page, title: string) => page.locator(`[data-search-hit="${title}"]`);
const palette = (page: Page) => page.getByRole("dialog", { name: "Quick search" });
const option = (page: Page, title: string) => palette(page).locator(`[data-quick-search-option="${title}"]`);
const paragraph = (text: string) => ({ type: "doc", content: [{ type: "paragraph", content: [{ type: "text", text }] }] });

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
    const zephyr = uniqueWord(testInfo, "zephyr");
    const flamingo = uniqueWord(testInfo, "flamingo");
    const title = `${zephyr} quarterly report`;
    await createPage(api, space.homePageId, title, paragraph(`The ${flamingo} budget grew this quarter.`));

    await openShowing(page, `/search?q=${zephyr}`, hit(page, title));
    await expect(hit(page, title).locator("mark").first()).toHaveText(zephyr);

    await page.locator("[data-search-query]").fill(flamingo);
    await page.locator("[data-search-query]").press("Enter");
    await expect(page).toHaveURL(new RegExp(`/search\\?q=${flamingo}$`));
    const found = hit(page, title);
    await expect(found.locator("[data-search-snippet] mark")).toHaveText(flamingo);
    await found.getByRole("link", { name: title }).click();
    await expect(page.locator("main").getByRole("heading", { level: 1 })).toHaveText(title);
  });

  test("narrows the hits to a space, and keeps the filter in the address", async ({ page, api }, testInfo) => {
    const here = await freshSpace(api, testInfo, "Here", "H");
    const there = await freshSpace(api, testInfo, "There", "T");
    const nebula = uniqueWord(testInfo, "nebula");
    await createPage(api, here.homePageId, `${nebula} notes here`);
    await createPage(api, there.homePageId, `${nebula} notes there`);

    await openShowing(page, `/search?q=${nebula}`, hit(page, `${nebula} notes there`));
    await expect(hit(page, `${nebula} notes here`)).toBeVisible();
    await page.locator('[data-filter="space"]').selectOption(here.key);
    await expect(page).toHaveURL(new RegExp(`space=${here.key}`));
    await expect(hit(page, `${nebula} notes here`)).toBeVisible();
    await expect(hit(page, `${nebula} notes there`)).toHaveCount(0);

    await page.reload();
    await expect(page.locator('[data-filter="space"]')).toHaveValue(here.key);
    await expect(hit(page, `${nebula} notes there`)).toHaveCount(0);
  });

  test("quick search opens a page from the keyboard, and Enter on the words opens the full search", async ({ page, api }, testInfo) => {
    const space = await freshSpace(api, testInfo, "Quick");
    const kestrel = uniqueWord(testInfo, "kestrel");
    const runbook = `${kestrel} runbook`;
    const target = await createPage(api, space.homePageId, runbook);
    await createPage(api, space.homePageId, `${kestrel} rota`);

    await page.goto("/");
    await expect(async () => {
      await page.keyboard.press("Control+k");
      await palette(page).getByRole("combobox").fill(`${kestrel} ru`);
      await expect(option(page, runbook)).toBeVisible(ONE_LOOK);
    }).toPass();
    const input = palette(page).getByRole("combobox");
    await expect(input).toBeFocused();
    await page.keyboard.press("ArrowDown");
    await expect(input).toHaveAttribute("aria-activedescendant", (await option(page, runbook).getAttribute("id")) ?? "");
    await expect(option(page, runbook)).toHaveAttribute("aria-selected", "true");
    await page.keyboard.press("Enter");
    await expect(page).toHaveURL(new RegExp(`/p/${target.id}/${kestrel}-runbook$`));
    await expect(palette(page)).toHaveCount(0);

    await page.keyboard.press("Control+k");
    await palette(page).getByRole("combobox").fill(kestrel);
    await page.keyboard.press("Enter");
    await expect(page).toHaveURL(new RegExp(`/search\\?q=${kestrel}$`));
    await expect(page.locator("[data-search-query]")).toHaveValue(kestrel);
  });

  test("quick search lists the pages opened last, the latest first", async ({ page, api }, testInfo) => {
    const space = await freshSpace(api, testInfo, "Recent");
    const first = await createPage(api, space.homePageId, "Heron minutes");
    const second = await createPage(api, space.homePageId, "Osprey minutes");

    for (const each of [first, second]) {
      // The page notes the visit after it shows; loading the next one before
      // the answer came cancels the note, and the page is never recent.
      const visited = page.waitForResponse((res) => res.url().endsWith(`/pages/${each.id}/visit`) && res.request().method() === "POST");
      await page.goto(`/spaces/${space.key}/p/${each.id}/${each.title.toLowerCase().replace(" ", "-")}`);
      await expect(page.locator("main").getByRole("heading", { level: 1 })).toHaveText(each.title);
      expect((await visited).ok()).toBe(true);
    }
    await openUntil(page, "/", async () => {
      await page.keyboard.press("Control+k");
      const recent = palette(page).locator('[data-quick-search-list="recent"] [role="option"]');
      await expect(recent.nth(1)).toBeVisible(ONE_LOOK);
      expect((await recent.allTextContents()).slice(0, 2).map((text) => text.split(space.name)[0])).toEqual(["Osprey minutes", "Heron minutes"]);
    });
  });

  test("a page in the trash is not found", async ({ page, api }, testInfo) => {
    const space = await freshSpace(api, testInfo, "Trashed");
    const walrus = uniqueWord(testInfo, "walrus");
    await createPage(api, space.homePageId, `${walrus} handover kept`);
    const gone = await createPage(api, space.homePageId, `${walrus} handover gone`);
    await api.DELETE("/pages/{pageID}", { params: { path: { pageID: gone.id } } });

    await openShowing(page, `/search?q=${walrus}`, hit(page, `${walrus} handover kept`));
    await expect(hit(page, `${walrus} handover gone`)).toHaveCount(0);

    await page.keyboard.press("Control+k");
    await palette(page).getByRole("combobox").fill(`${walrus} handover`);
    await expect(option(page, `${walrus} handover kept`)).toBeVisible();
    await expect(option(page, `${walrus} handover gone`)).toHaveCount(0);
  });

  test("corrects a mistyped word, and searches for it as typed when asked", async ({ page, api }, testInfo) => {
    const space = await freshSpace(api, testInfo, "Typo");
    const word = uniqueWord(testInfo, "kubernetes");
    const typo = word.replace("kubernetes", "kubernets");
    const title = `${word} upgrade`;
    await createPage(api, space.homePageId, title);

    await openShowing(page, `/search?q=${typo}`, hit(page, title));
    const notice = page.locator("[data-search-corrected]");
    await expect(notice).toHaveText(`Showing results for ${word}. Search instead for ${typo}`);
    await expect(notice.locator("strong")).toHaveText(word);
    await expect(hit(page, title).locator("mark").first()).toHaveText(word);
    await expect(page.locator("[data-search-query]")).toHaveValue(typo);

    await notice.getByRole("link", { name: typo }).click();
    await expect(page).toHaveURL(new RegExp(`/search\\?q=${typo}&asTyped=true$`));
    await expect(page.getByText("Nothing matches")).toBeVisible();
    await expect(hit(page, title)).toHaveCount(0);
    await expect(notice).toHaveCount(0);
  });

  for (const scheme of ["light", "dark"] as ColourScheme[]) {
    test(`the search page and quick search pass axe in ${scheme}`, async ({ page, api }, testInfo) => {
      const space = await freshSpace(api, testInfo, "Axe");
      const puffin = uniqueWord(testInfo, "puffin");
      await createPage(api, space.homePageId, `${puffin} guide`, paragraph(`Where the ${puffin} nests.`));
      await startInScheme(page, scheme);

      await openShowing(page, `/search?q=${puffin}`, hit(page, `${puffin} guide`));
      await expect(page.locator("html")).toHaveAttribute("data-theme", scheme);
      await expectAccessible(page);

      await page.keyboard.press("Control+k");
      await palette(page).getByRole("combobox").fill(puffin);
      await expect(option(page, `${puffin} guide`)).toBeVisible();
      await page.keyboard.press("ArrowDown");
      await expectAccessible(page);
    });
  }
});
