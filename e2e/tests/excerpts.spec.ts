import type { Page, TestInfo } from "@playwright/test";
import type { StatorApi } from "../fixtures/api";
import { must } from "../fixtures/api";
import { expect } from "../fixtures/auth";
import { caretTo } from "../fixtures/editor";
import { orgTest as test } from "../fixtures/org";
import { uniqueName } from "../fixtures/seed";
import { expectAccessible } from "../fixtures/shell";
import { createPage, createSpace, deleteSpace, publishFromEditor, uniqueKey, type Space } from "../fixtures/spaces";

const editorBox = (page: Page) => page.locator("#page-body");
const shown = (page: Page) => page.locator("[data-doc]");

const paragraph = (value: string) => ({ type: "paragraph", content: [{ type: "text", text: value }] });

/** Opens a page's editor until it holds the words given, which a replica may lag behind on. */
async function openEditor(page: Page, path: string, words: string): Promise<void> {
  await expect(async () => {
    await page.goto(`${path}/edit`);
    await expect(editorBox(page)).toContainText(words, { timeout: 2_000 });
  }).toPass();
}

test.describe("excerpts", { tag: ["@auth"] }, () => {
  const made: string[] = [];
  test.afterEach(async ({ api }) => {
    for (const key of made.splice(0)) await deleteSpace(api, key);
  });

  async function freshSpace(api: StatorApi, testInfo: TestInfo, label: string): Promise<Space> {
    const key = uniqueKey(testInfo);
    made.push(key);
    return createSpace(api, key, uniqueName(testInfo, label));
  }

  test("an author names part of a page as an excerpt, which readers read as the page and pickers list by name", async ({ page, api }, testInfo) => {
    const space = await freshSpace(api, testInfo, "Excerpts");
    const support = await createPage(api, space.homePageId, "Support", {
      type: "doc",
      content: [paragraph("Who we are."), paragraph("Nine to five on weekdays.")],
    });
    const path = `/s/${space.key}/p/${support.id}/support`;

    await openEditor(page, path, "Nine to five on weekdays.");
    await caretTo(editorBox(page), "end");
    await page.keyboard.type(" /excerpt");
    await expect(page.getByRole("listbox", { name: "Insert a block" })).toBeVisible();
    await page.keyboard.press("Enter");
    const name = editorBox(page).getByLabel("Excerpt");
    await expect(name).toBeFocused();
    await expect(name).toHaveValue("Excerpt 1");
    await page.keyboard.type("Support hours");
    await expect(editorBox(page).locator("[data-excerpt] [data-excerpt-body]")).toContainText("Nine to five on weekdays.");
    await expect(editorBox(page).locator("[data-excerpt] [data-excerpt-body]")).not.toContainText("Who we are.");
    await expectAccessible(page);

    await expect(page.locator("[data-draft-status]")).toHaveAttribute("data-draft-status", "saved");
    await publishFromEditor(page);

    await expect(shown(page).locator("[data-excerpt] p")).toHaveText("Nine to five on weekdays.");
    await expect(shown(page)).not.toContainText("Support hours");

    const excerpts = must(await api.GET("/pages/{pageID}/excerpts", { params: { path: { pageID: support.id } } })).excerpts;
    expect(excerpts.map((each) => [each.name, each.text])).toEqual([["Support hours", "Nine to five on weekdays."]]);
  });
});
