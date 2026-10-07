import type { Page } from "@playwright/test";
import { expect } from "../fixtures/auth";
import { caretTo } from "../fixtures/editor";
import { orgTest as test } from "../fixtures/org";
import { openEditor } from "../fixtures/replica";
import { uniqueName } from "../fixtures/seed";
import { createPage, createSpace, deleteSpace, uniqueKey } from "../fixtures/spaces";

const editorBox = (page: Page) => page.locator("#page-body");
const status = (page: Page) => page.locator("[data-draft-status]");

/** Notes where the page body's top edge is each time the save status changes. */
async function watchBodyTop(page: Page) {
  await page.evaluate(() => {
    const tops: Array<[string, number]> = [];
    (window as unknown as { bodyTops: typeof tops }).bodyTops = tops;
    const label = document.querySelector("[data-draft-status]")!;
    const body = document.querySelector("#page-body")!;
    const note = () => tops.push([label.getAttribute("data-draft-status") ?? "", body.getBoundingClientRect().top + window.scrollY]);
    note();
    new MutationObserver(note).observe(label, { attributes: true, childList: true, characterData: true, subtree: true });
  });
}

async function bodyTops(page: Page): Promise<Array<[string, number]>> {
  return page.evaluate(() => (window as unknown as { bodyTops: Array<[string, number]> }).bodyTops);
}

test.describe("the editor's save status", { tag: ["@auth"] }, () => {
  const made: string[] = [];
  test.afterEach(async ({ api }) => {
    for (const key of made.splice(0)) await deleteSpace(api, key);
  });

  test("changing between its messages does not move the page being typed", async ({ page, api }, testInfo) => {
    const key = uniqueKey(testInfo);
    made.push(key);
    const space = await createSpace(api, key, uniqueName(testInfo, "Status"));
    const notes = await createPage(api, space.homePageId, "Standing notes", {
      type: "doc",
      content: [{ type: "paragraph", content: [{ type: "text", text: "Agenda first." }] }],
    });

    await openEditor(page, `/s/${space.key}/p/${notes.id}/standing-notes`, "Agenda first.");
    await watchBodyTop(page);
    await caretTo(editorBox(page), "end");
    await page.keyboard.type(" Then the minutes.");
    await expect(status(page)).toHaveAttribute("data-draft-status", "saved");
    await page.keyboard.type(" And the actions.");
    await expect(status(page)).toHaveAttribute("data-draft-status", "saved");

    const tops = await bodyTops(page);
    expect(new Set(tops.map(([state]) => state))).toEqual(new Set(["idle", "pending", "saving", "saved"]));
    expect(new Set(tops.map(([, top]) => Math.round(top))).size, JSON.stringify(tops)).toBe(1);
  });
});
