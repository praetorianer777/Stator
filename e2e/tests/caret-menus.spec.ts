import type { Page, TestInfo } from "@playwright/test";
import type { StatorApi } from "../fixtures/api";
import { expect } from "../fixtures/auth";
import { caretTo } from "../fixtures/editor";
import { orgTest as test } from "../fixtures/org";
import { ONE_LOOK, openUntil } from "../fixtures/replica";
import { uniqueName } from "../fixtures/seed";
import { createPage, createSpace, deleteSpace, uniqueKey } from "../fixtures/spaces";

const editorBox = (page: Page) => page.locator("#page-body");

// Enough lines that the last one is far below the fold once the page is scrolled to its end.
const LINES = 60;
const paragraph = (value: string) => ({ type: "paragraph", content: [{ type: "text", text: value }] });

/** The box of an element, which a test asks lies wholly inside the window. */
async function expectInsideWindow(page: Page, selector: string) {
  const box = await page.locator(selector).boundingBox();
  const size = page.viewportSize()!;
  expect(box, `${selector} is drawn`).not.toBeNull();
  expect(box!.y, "top edge").toBeGreaterThanOrEqual(0);
  expect(box!.y + box!.height, "bottom edge").toBeLessThanOrEqual(size.height);
  expect(box!.x, "left edge").toBeGreaterThanOrEqual(0);
  expect(box!.x + box!.width, "right edge").toBeLessThanOrEqual(size.width);
}

test.describe("menus at the caret", { tag: ["@auth", "@desktop"] }, () => {
  const made: string[] = [];
  test.afterEach(async ({ api }) => {
    for (const key of made.splice(0)) await deleteSpace(api, key);
  });

  async function longPage(api: StatorApi, testInfo: TestInfo) {
    const key = uniqueKey(testInfo);
    made.push(key);
    const space = await createSpace(api, key, uniqueName(testInfo, "Caret"));
    const content = Array.from({ length: LINES }, (_, i) => paragraph(`Line ${i + 1} of a page that is longer than the window.`));
    const page = await createPage(api, space.homePageId, "Long", { type: "doc", content });
    return { space, page };
  }

  test("the slash menu stays inside the window with the caret on the last line", async ({ page, api }, testInfo) => {
    const { space, page: doc } = await longPage(api, testInfo);
    await openUntil(page, `/s/${space.key}/p/${doc.id}/long/edit`, () => expect(editorBox(page)).toContainText(`Line ${LINES}`, ONE_LOOK));
    await caretTo(editorBox(page), "end");
    await page.keyboard.press("Enter");
    await page.keyboard.type("/");
    await expect(page.getByRole("listbox", { name: "Insert a block" })).toBeVisible();
    await expectInsideWindow(page, "[data-slash-menu]");

    // Typing narrows it, and the first match still picks with Enter.
    await page.keyboard.type("head");
    await expectInsideWindow(page, "[data-slash-menu]");
    await page.keyboard.press("Enter");
    await expect(editorBox(page).getByRole("heading").last()).toBeVisible();
  });

  test("it opens below the caret when the caret is near the top", async ({ page, api }, testInfo) => {
    const { space, page: doc } = await longPage(api, testInfo);
    await openUntil(page, `/s/${space.key}/p/${doc.id}/long/edit`, () => expect(editorBox(page)).toContainText("Line 1 ", ONE_LOOK));
    await caretTo(editorBox(page), "start");
    await page.keyboard.type("/");
    const menu = page.locator("[data-slash-menu]");
    await expect(menu).toBeVisible();
    await expectInsideWindow(page, "[data-slash-menu]");
    const caretBottom = await page.evaluate(() => window.getSelection()!.getRangeAt(0).getBoundingClientRect().bottom);
    expect((await menu.boundingBox())!.y).toBeGreaterThanOrEqual(caretBottom);
  });
});
