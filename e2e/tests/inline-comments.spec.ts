import { randomUUID } from "node:crypto";
import type { Page, TestInfo } from "@playwright/test";
import { must, type StatorApi } from "../fixtures/api";
import { expect } from "../fixtures/auth";
import { caretTo } from "../fixtures/editor";
import { orgTest as test } from "../fixtures/org";
import { ONE_LOOK, openPage } from "../fixtures/replica";
import { uniqueName } from "../fixtures/seed";
import { expectAccessible, startInScheme, type ColourScheme } from "../fixtures/shell";
import { createPage, createSpace, deleteSpace, publishFromEditor, uniqueKey, type Page as WikiPage } from "../fixtures/spaces";

const passage = (page: Page, id?: string) => page.locator(id ? `mark[data-passage="${id}"]` : "mark[data-passage]");
const panel = (page: Page) => page.locator("[data-passage-panel]");
const listed = (page: Page) => page.locator("[data-passages]");
const draftStatus = (page: Page) => page.locator("[data-draft-status]");

const doc = (...lines: string[]) => ({ type: "doc" as const, content: lines.map((text) => ({ type: "paragraph", content: [{ type: "text", text }] })) });

/** Selects words of the page's first paragraph, as dragging over them would. */
async function select(page: Page, words: string) {
  await page.evaluate((wanted) => {
    const block = document.querySelector('[data-passage-root] [data-block="0"]')!;
    const walker = document.createTreeWalker(block, NodeFilter.SHOW_TEXT);
    for (let node = walker.nextNode() as Text | null; node; node = walker.nextNode() as Text | null) {
      const at = node.data.indexOf(wanted);
      if (at < 0) continue;
      const range = document.createRange();
      range.setStart(node, at);
      range.setEnd(node, at + wanted.length);
      const selection = window.getSelection()!;
      selection.removeAllRanges();
      selection.addRange(range);
      return;
    }
    throw new Error(`${wanted} is not in the first paragraph`);
  }, words);
}

/** Writes in the comment editor that opened last and posts it. */
async function post(page: Page, editor: string, text: string) {
  const box = page.locator(`#${editor}`);
  await expect(box).toBeVisible();
  await box.click();
  await page.keyboard.type(text);
  await page.keyboard.press("ControlOrMeta+Enter");
  await expect(box).toHaveCount(0);
}

/** Starts a thread on a passage of the first paragraph through the API, as the reader does. */
async function startInline(api: StatorApi, target: WikiPage, before: string, words: string, after: string, text: string): Promise<string> {
  const threadId = randomUUID();
  const pageBody = {
    type: "doc" as const,
    content: [
      {
        type: "paragraph",
        content: [
          { type: "text", text: before },
          { type: "text", text: words, marks: [{ type: "inlineComment", attrs: { threadId } }] },
          { type: "text", text: after },
        ],
      },
    ],
  };
  must(await api.POST("/pages/{pageID}/inline-comments", { params: { path: { pageID: target.id } }, body: { threadId, body: doc(text), pageBody } }));
  return threadId;
}

test.describe("comments on passages", { tag: ["@auth"] }, () => {
  const made: string[] = [];
  test.afterEach(async ({ api }) => {
    for (const key of made.splice(0)) await deleteSpace(api, key);
  });

  async function freshSpace(api: StatorApi, testInfo: TestInfo, label: string) {
    const key = uniqueKey(testInfo);
    made.push(key);
    return createSpace(api, key, uniqueName(testInfo, label));
  }

  test("alice comments on a passage, bob replies and resolves, and her edit detaches it", async ({ page, api, pageAs }, testInfo) => {
    test.slow();
    const space = await freshSpace(api, testInfo, "Passages");
    const plan = await createPage(api, space.homePageId, uniqueName(testInfo, "Plan"), doc("Ship on Friday.", "Notes follow."));

    await openPage(page, space.key, plan);
    await select(page, "on Friday");
    await page.locator('[data-action="comment-on-selection"]').click();
    await expect(panel(page)).toContainText("on Friday");
    await post(page, "new-passage-comment", "Why not Monday?");
    await expect(passage(page)).toHaveText("on Friday");
    await expect(panel(page)).toContainText("Why not Monday?");
    const threadId = await passage(page).getAttribute("data-passage");

    const bob = await pageAs("bob");
    await openPage(bob, space.key, plan, () => expect(passage(bob)).toHaveCount(1, ONE_LOOK));
    await passage(bob).click();
    await expect(panel(bob)).toBeFocused();
    await panel(bob).locator('[data-action="reply"]').click();
    await post(bob, `reply-${threadId}`, "Monday is safer.");
    await expect(panel(bob)).toContainText("Monday is safer.");
    await panel(bob).locator('[data-action="resolve-thread"]').click();
    await expect(panel(bob)).toHaveCount(0);
    await expect(passage(bob)).toHaveCount(0);
    await expect(listed(bob).locator('[data-action="toggle-resolved"]')).toHaveText("Show 1 resolved thread");

    // A draft carries the mark the editor loaded; a comparison does not show it.
    await page.goto(`/s/${space.key}/p/${plan.id}/plan/edit`);
    await expect(page.locator("#page-body")).toContainText("Ship on Friday.");
    await caretTo(page.locator("#page-body"), "end");
    await page.keyboard.type(" Soon.");
    await expect(draftStatus(page)).toHaveAttribute("data-draft-status", /^(saving|saved)$/);
    await expect(draftStatus(page)).toHaveAttribute("data-draft-status", "saved");
    await page.goto(`/s/${space.key}/p/${plan.id}/plan/history?from=1&to=draft`);
    const diff = page.locator("[data-diff-view]");
    await expect(diff.locator("ins[data-diff]").filter({ hasText: "Soon" }).first()).toBeVisible();
    await expect(diff).toContainText("Ship on Friday.");
    await expect(diff.locator("mark, [data-inline-comment], ins, del").filter({ hasText: "Friday" })).toHaveCount(0);

    // Rewriting the passage leaves its thread nothing to point at.
    await page.goto(`/s/${space.key}/p/${plan.id}/plan/edit`);
    await caretTo(page.locator("#page-body"), "start");
    await page.keyboard.press("Shift+End");
    await page.keyboard.type("Ship on Monday.");
    await publishFromEditor(page, "Monday it is.");
    await expect(page.locator("[data-doc]")).toContainText("Ship on Monday.");
    await expect(passage(page)).toHaveCount(0);
    await listed(page).locator('[data-action="toggle-resolved"]').click();
    const detached = listed(page).locator(`[data-thread="${threadId}"]`);
    await expect(detached).toContainText("on Friday");
    await expect(detached).toContainText("Monday is safer.");
    await expect(listed(page).locator(`[data-anchor-state="detached"]`)).toHaveCount(1);
    await expect(listed(page)).toContainText("Passages no longer on the page");
  });

  for (const scheme of ["light", "dark"] as ColourScheme[]) {
    test(`passages and their threads are accessible and keyboard usable in ${scheme}`, async ({ page, api }, testInfo) => {
      const space = await freshSpace(api, testInfo, `Axe ${scheme}`);
      const plan = await createPage(api, space.homePageId, uniqueName(testInfo, "Checked"), doc("We ship on Friday after the review."));
      const threadId = await startInline(api, plan, "We ", "ship on Friday", " after the review.", "Why Friday?");
      await startInScheme(page, scheme);
      await openPage(page, space.key, plan, () => expect(passage(page, threadId)).toBeVisible(ONE_LOOK));
      await expectAccessible(page);

      const opener = listed(page).locator(`[data-open-passage="${threadId}"]`);
      await opener.focus();
      await page.keyboard.press("Enter");
      await expect(panel(page)).toBeFocused();
      await expect(passage(page, threadId)).toHaveAttribute("data-active", "true");
      await expectAccessible(page);
      await page.keyboard.press("Escape");
      await expect(panel(page)).toHaveCount(0);

      await select(page, "after the review");
      await expect(page.locator('[data-action="comment-on-selection"]')).toBeVisible();
      await page.keyboard.press("ControlOrMeta+Alt+KeyM");
      const box = page.locator("#new-passage-comment");
      await expect(box).toBeFocused();
      await expectAccessible(page);
      await page.keyboard.type("Typed without a mouse.");
      await page.keyboard.press("ControlOrMeta+Enter");
      await expect(passage(page)).toHaveCount(2);
      await expect(panel(page)).toBeFocused();
      await expect(panel(page)).toContainText("Typed without a mouse.");
    });
  }
});
