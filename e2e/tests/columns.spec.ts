import type { Locator, Page, TestInfo } from "@playwright/test";
import type { StatorApi } from "../fixtures/api";
import { expect } from "../fixtures/auth";
import { caretTo } from "../fixtures/editor";
import { orgTest as test } from "../fixtures/org";
import { uniqueName } from "../fixtures/seed";
import { expectAccessible, startInScheme, type ColourScheme } from "../fixtures/shell";
import { createPage, createSpace, deleteSpace, publishFromEditor, uniqueKey, type Space } from "../fixtures/spaces";

const editorBox = (page: Page) => page.locator("#page-body");
const shown = (page: Page) => page.locator("[data-doc]");

const text = (value: string) => [{ type: "text", text: value }];
const paragraph = (value: string) => ({ type: "paragraph", content: text(value) });
const column = (width: number, words: string) => ({ type: "column", attrs: { width }, content: [paragraph(words)] });

// How far apart in pixels two boxes may be and still count as level, or as
// sized in the proportion asked: borders and the gap round the rest away.
const LEVEL_PX = 2;
const SHARE_TOLERANCE = 0.05;

async function box(target: Locator) {
  const b = await target.boundingBox();
  if (!b) throw new Error("The element has no box; it is not laid out.");
  return b;
}

/** Opens a page's editor until it holds the words given, which a replica may lag behind on. */
async function openEditor(page: Page, path: string, words: string): Promise<void> {
  await expect(async () => {
    await page.goto(`${path}/edit`);
    await expect(editorBox(page)).toContainText(words, { timeout: 2_000 });
  }).toPass();
}

/** Opens a page until it shows the words given. */
async function openPage(page: Page, path: string, words: string): Promise<void> {
  await expect(async () => {
    await page.goto(path);
    await expect(shown(page)).toContainText(words, { timeout: 2_000 });
  }).toPass();
}

test.describe("column layouts", () => {
  const made: string[] = [];
  test.afterEach(async ({ api }) => {
    for (const key of made.splice(0)) await deleteSpace(api, key);
  });

  async function freshSpace(api: StatorApi, testInfo: TestInfo, label: string): Promise<Space> {
    const key = uniqueKey(testInfo);
    made.push(key);
    return createSpace(api, key, uniqueName(testInfo, label));
  }

  test("three go in from the slash menu, take a layout of two and read side by side", { tag: ["@auth", "@desktop"] }, async ({ page, api }, testInfo) => {
    const space = await freshSpace(api, testInfo, "Columns");
    // The empty paragraph at the end takes the block: an Enter after the caret
    // key would split wherever ProseMirror last saw the caret.
    const compare = await createPage(api, space.homePageId, "Compare", { type: "doc", content: [paragraph("Two options."), { type: "paragraph" }] });
    const path = `/s/${space.key}/p/${compare.id}/compare`;

    await openEditor(page, path, "Two options.");
    await caretTo(editorBox(page), "end");
    await page.keyboard.type("/three");
    await expect(page.getByRole("listbox", { name: "Insert a block" })).toBeVisible();
    await page.keyboard.press("Enter");
    const columns = editorBox(page).locator("[data-columns] > [data-column]");
    await expect(columns).toHaveCount(3);
    await page.keyboard.type("Option A keeps the schema.");
    await columns.nth(1).click();
    await page.keyboard.type("Option B splits the table.");

    const layout = page.locator('[data-editor-tools="columns"]').getByRole("combobox", { name: "Column layout" });
    await expect(layout).toHaveValue("threeEven");
    await layout.selectOption({ label: "Two columns, wider right" });
    await expect(columns).toHaveCount(2);
    await expect(layout).toHaveValue("twoWideRight");
    await expect(page.locator("[data-draft-status]")).toHaveAttribute("data-draft-status", "saved");
    await publishFromEditor(page);

    const left = shown(page).locator("[data-columns] > [data-column]").first();
    const right = shown(page).locator("[data-columns] > [data-column]").last();
    await expect(left).toHaveText("Option A keeps the schema.");
    await expect(right).toHaveText("Option B splits the table.");
    const [l, r] = [await box(left), await box(right)];
    expect(Math.abs(l.y - r.y)).toBeLessThanOrEqual(LEVEL_PX);
    expect(r.x).toBeGreaterThan(l.x + l.width);
    expect(Math.abs(r.width / (l.width + r.width) - 0.67)).toBeLessThanOrEqual(SHARE_TOLERANCE);

    await openEditor(page, path, "Option B");
    await editorBox(page).getByText("Option A keeps the schema.").click();
    await page.locator('[data-editor-tools="columns"] [data-editor-action="remove-columns"]').click();
    await expect(editorBox(page).locator("[data-columns]")).toHaveCount(0);
    await expect(editorBox(page)).toContainText("Option A keeps the schema.");
    await expect(editorBox(page)).toContainText("Option B splits the table.");
  });

  test("they stack in reading order on a phone", { tag: ["@auth", "@mobile"] }, async ({ page, api }, testInfo) => {
    const space = await freshSpace(api, testInfo, "Stack");
    const notes = await createPage(api, space.homePageId, "Notes", {
      type: "doc",
      content: [{ type: "columns", content: [column(25, "First column."), column(50, "Second column."), column(25, "Third column.")] }],
    });
    await openPage(page, `/s/${space.key}/p/${notes.id}/notes`, "Third column.");

    const parts = shown(page).locator("[data-columns] > [data-column]");
    const boxes = [await box(parts.nth(0)), await box(parts.nth(1)), await box(parts.nth(2))];
    for (let i = 1; i < boxes.length; i++) {
      expect(boxes[i]!.y).toBeGreaterThanOrEqual(boxes[i - 1]!.y + boxes[i - 1]!.height);
      expect(Math.abs(boxes[i]!.width - boxes[0]!.width)).toBeLessThanOrEqual(LEVEL_PX);
    }
    expect(await page.evaluate(() => document.documentElement.scrollWidth > document.documentElement.clientWidth)).toBe(false);
  });

  for (const scheme of ["light", "dark"] as ColourScheme[]) {
    test(`columns pass axe in ${scheme}, read and in the editor`, { tag: ["@auth", "@desktop"] }, async ({ page, api }, testInfo) => {
      const space = await freshSpace(api, testInfo, "Axe");
      const notes = await createPage(api, space.homePageId, "Notes", {
        type: "doc",
        content: [paragraph("Side by side."), { type: "columns", content: [column(50, "On the left."), column(50, "On the right.")] }],
      });
      await startInScheme(page, scheme);
      const path = `/s/${space.key}/p/${notes.id}/notes`;

      await openPage(page, path, "On the right.");
      await expect(page.locator("html")).toHaveAttribute("data-theme", scheme);
      await expectAccessible(page);

      await openEditor(page, path, "On the right.");
      await editorBox(page).getByText("On the left.").click();
      await expect(page.locator('[data-editor-tools="columns"]')).toBeVisible();
      await expectAccessible(page);
    });
  }
});
