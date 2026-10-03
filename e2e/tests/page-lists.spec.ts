import type { Page, TestInfo } from "@playwright/test";
import { must, type StatorApi } from "../fixtures/api";
import { expect } from "../fixtures/auth";
import { caretTo } from "../fixtures/editor";
import { orgTest as test } from "../fixtures/org";
import { uniqueName } from "../fixtures/seed";
import { expectAccessible, startInScheme, type ColourScheme } from "../fixtures/shell";
import { createPage, createSpace, deleteSpace, publishFromEditor, uniqueKey, type Space } from "../fixtures/spaces";

const editorBox = (page: Page) => page.locator("#page-body");
const shown = (page: Page) => page.locator("main [data-doc]");
const paragraph = (value: string) => ({ type: "paragraph", content: [{ type: "text", text: value }] });

/** Opens a page until it holds the words given, which a replica may lag behind on. */
async function open(page: Page, path: string, words: string): Promise<void> {
  await expect(async () => {
    await page.goto(path);
    await expect(page.locator("main")).toContainText(words, { timeout: 2_000 });
  }).toPass();
}

async function label(api: StatorApi, pageId: string, name: string): Promise<void> {
  must(await api.POST("/pages/{pageID}/labels", { params: { path: { pageID: pageId } }, body: { name } }));
}

test.describe("page lists", { tag: ["@auth"] }, () => {
  const made: string[] = [];
  test.afterEach(async ({ api }) => {
    for (const key of made.splice(0)) await deleteSpace(api, key);
  });

  async function freshSpace(api: StatorApi, testInfo: TestInfo, title: string): Promise<Space> {
    const key = uniqueKey(testInfo);
    made.push(key);
    return createSpace(api, key, uniqueName(testInfo, title));
  }

  test("an overview page lists the pages by label and the latest updates, and stays current without editing", async ({ page, api }, testInfo) => {
    const space = await freshSpace(api, testInfo, "Overview");
    // Labels are the organization's, so each run takes its own.
    const tag = `howto-${space.key.toLowerCase()}`;
    const guide = await createPage(api, space.homePageId, "Install guide", { type: "doc", content: [paragraph("Steps.")] });
    await label(api, guide.id, tag);
    const runbook = await createPage(api, space.homePageId, "Runbook", { type: "doc", content: [paragraph("On call.")] });
    await label(api, runbook.id, tag);
    const overview = await createPage(api, space.homePageId, "Overview", { type: "doc", content: [paragraph("Start here."), { type: "paragraph" }] });
    const path = `/s/${space.key}/p/${overview.id}/overview`;

    await open(page, `${path}/edit`, "Start here.");
    await caretTo(editorBox(page), "end");
    await page.keyboard.type("/tagged");
    await expect(page.getByRole("listbox", { name: "Insert a block" })).toBeVisible();
    await page.keyboard.press("Enter");
    const dialog = page.getByRole("dialog", { name: "Insert a list of pages by label" });
    await dialog.getByLabel("Labels", { exact: true }).fill(tag.toUpperCase());
    await dialog.getByLabel("Space").selectOption(space.key);
    await dialog.getByLabel("Order").selectOption("title");
    await dialog.getByRole("button", { name: "Insert" }).click();
    await expect(editorBox(page).locator('[data-page-list="labelled"] [data-listed-page]')).toHaveText([/Install guide/, /Runbook/]);

    await caretTo(editorBox(page), "end");
    await page.keyboard.type("/recently");
    await expect(page.getByRole("listbox", { name: "Insert a block" })).toBeVisible();
    await page.keyboard.press("Enter");
    // It lists everywhere at first; its settings keep it to this space.
    const updated = editorBox(page).locator('[data-page-list="updated"]');
    await updated.locator("xpath=ancestor::*[@data-recently-updated-node]").getByRole("button", { name: "Edit list" }).click();
    const edit = page.getByRole("dialog", { name: "Edit the list of recently updated pages" });
    await edit.getByLabel("Space").selectOption(space.key);
    await edit.getByLabel("Show").selectOption("5");
    await edit.getByRole("button", { name: "Save" }).click();
    await expect(editorBox(page).getByRole("region", { name: `Recently updated in ${space.key}` })).toBeVisible();
    await expect(page.locator("[data-draft-status]")).toHaveAttribute("data-draft-status", "saved");
    await publishFromEditor(page);

    const byLabel = shown(page).locator('[data-page-list="labelled"]');
    await expect(byLabel.getByRole("link")).toHaveText(["Install guide", "Runbook"]);
    const latest = shown(page).getByRole("region", { name: `Recently updated in ${space.key}` });
    await expect(latest.locator("[data-listed-page]").first()).toHaveAttribute("data-listed-page", "Overview");

    // Somebody revises the guide elsewhere; the overview shows it without being edited.
    must(await api.PATCH("/pages/{pageID}", { params: { path: { pageID: guide.id } }, body: { title: "Install guide, revised", version: 1 } }));
    await expect(async () => {
      await page.reload();
      await expect(latest.locator("[data-listed-page]").first()).toHaveAttribute("data-listed-page", "Install guide, revised", { timeout: 2_000 });
    }).toPass();
    await byLabel.getByRole("link", { name: "Runbook" }).click();
    await expect(page).toHaveURL(new RegExp(`/p/${runbook.id}/`));
  });

  for (const scheme of ["light", "dark"] as ColourScheme[]) {
    test(`page lists pass axe in ${scheme}`, async ({ page, api }, testInfo) => {
      const space = await freshSpace(api, testInfo, "Axe");
      const tag = `axe-${space.key.toLowerCase()}`;
      const one = await createPage(api, space.homePageId, "Labelled one", { type: "doc", content: [paragraph("x")] });
      await label(api, one.id, tag);
      const overview = await createPage(api, space.homePageId, "Lists", {
        type: "doc",
        content: [
          { type: "labelledPages", attrs: { labels: [tag], match: "all", space: null, sort: "updated", limit: 10 } },
          { type: "recentlyUpdated", attrs: { space: space.key, limit: 5 } },
          { type: "labelledPages", attrs: { labels: [`${tag}-none`], match: "any", space: null, sort: "title", limit: 10 } },
        ],
      });
      await startInScheme(page, scheme);
      await open(page, `/s/${space.key}/p/${overview.id}/lists`, "Labelled one");
      await expect(shown(page).locator('[data-page-list][data-state="empty"]')).toHaveCount(1);
      await page.screenshot({ path: testInfo.outputPath(`lists-${scheme}.png`), fullPage: true });
      await expectAccessible(page);
    });
  }
});
