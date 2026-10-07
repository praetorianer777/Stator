import type { Page, TestInfo } from "@playwright/test";
import type { StatorApi } from "../fixtures/api";
import { expect } from "../fixtures/auth";
import { caretTo } from "../fixtures/editor";
import { orgTest as test } from "../fixtures/org";
import { openEditor, openShowing } from "../fixtures/replica";
import { uniqueName } from "../fixtures/seed";
import { expectAccessible, startInScheme, type ColourScheme } from "../fixtures/shell";
import { createPage, createSpace, deleteSpace, publishFromEditor, uniqueKey, type Space } from "../fixtures/spaces";

const editorBox = (page: Page) => page.locator("#page-body");
const shown = (page: Page) => page.locator("[data-doc]");
const dialog = (page: Page) => page.getByRole("dialog", { name: "Formula" });

const paragraph = (value: string) => ({ type: "paragraph", content: [{ type: "text", text: value }] });

/** Writes a formula's source in the open dialog and saves it. */
async function writeFormula(page: Page, latex: string): Promise<void> {
  const source = dialog(page).getByLabel("LaTeX source");
  await source.fill(latex);
  await expect(dialog(page).locator("[data-math-preview] .katex")).toBeVisible();
  await dialog(page).getByRole("button", { name: "Save" }).click();
  await expect(dialog(page)).toHaveCount(0);
}

test.describe("math formulas", { tag: ["@auth"] }, () => {
  const made: string[] = [];
  test.afterEach(async ({ api }) => {
    for (const key of made.splice(0)) await deleteSpace(api, key);
  });

  async function freshSpace(api: StatorApi, testInfo: TestInfo, label: string): Promise<Space> {
    const key = uniqueKey(testInfo);
    made.push(key);
    return createSpace(api, key, uniqueName(testInfo, label));
  }

  test("an author writes formulas, typeset as they type, and changes one with a click", async ({ page, api }, testInfo) => {
    const space = await freshSpace(api, testInfo, "Maths");
    // The empty paragraph at the end takes the block: an Enter after the caret
    // key would split wherever ProseMirror last saw the caret.
    const notes = await createPage(api, space.homePageId, "Geometry", { type: "doc", content: [paragraph("A circle's area."), { type: "paragraph" }] });
    const path = `/s/${space.key}/p/${notes.id}/geometry`;

    await openEditor(page, path, "A circle's area.");
    await caretTo(editorBox(page), "end");
    await page.keyboard.type("/formula");
    await expect(page.getByRole("listbox", { name: "Insert a block" })).toBeVisible();
    await page.keyboard.press("Enter");
    await expect(dialog(page)).toBeVisible();
    await dialog(page).getByLabel("LaTeX source").fill("\\frac{a");
    await expect(dialog(page).getByText(/This formula cannot be typeset/)).toBeVisible();
    await writeFormula(page, "A = \\pi r^2");
    await expect(editorBox(page).locator('[data-math-edit="block"] .katex-display')).toBeVisible();

    await caretTo(editorBox(page), "end");
    await page.keyboard.type("Half of it is ");
    await page.keyboard.type("/inline");
    await page.keyboard.press("Enter");
    await writeFormula(page, "\\tfrac{1}{2}A");
    await expect(editorBox(page).locator('[data-math-edit="inline"] .katex')).toBeVisible();

    // Its source opens again on a click, as the author left it.
    await editorBox(page).locator('[data-math-edit="block"]').click();
    await expect(dialog(page).getByLabel("LaTeX source")).toHaveValue("A = \\pi r^2");
    await writeFormula(page, "A = \\pi r^{2}");

    await expect(page.locator("[data-draft-status]")).toHaveAttribute("data-draft-status", "saved");
    await publishFromEditor(page);

    await expect(shown(page).locator('[data-math="block"] .katex-display')).toBeVisible();
    await expect(shown(page).locator('[data-math="block"] annotation')).toHaveText("A = \\pi r^{2}");
    await expect(shown(page).locator('[data-math="inline"] annotation')).toHaveText("\\tfrac{1}{2}A");
  });

  for (const scheme of ["light", "dark"] as ColourScheme[]) {
    test(`formulas pass axe in ${scheme}, a broken one too`, async ({ page, api }, testInfo) => {
      const space = await freshSpace(api, testInfo, "Axe");
      const notes = await createPage(api, space.homePageId, "Notes", {
        type: "doc",
        content: [
          {
            type: "paragraph",
            content: [
              { type: "text", text: "Energy is " },
              { type: "mathInline", attrs: { latex: "E = mc^2" } },
            ],
          },
          { type: "mathBlock", attrs: { latex: "\\sum_{i=1}^{n} i = \\frac{n(n+1)}{2}" } },
          { type: "mathBlock", attrs: { latex: "\\frac{a" } },
        ],
      });
      await startInScheme(page, scheme);
      await openShowing(page, `/s/${space.key}/p/${notes.id}/notes`, shown(page).locator(".katex-display"));
      await expect(shown(page).locator("[data-math-error]")).toContainText("\\frac{a");
      await expectAccessible(page);
    });
  }
});
