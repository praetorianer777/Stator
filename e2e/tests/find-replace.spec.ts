import type { Page, TestInfo } from "@playwright/test";
import type { StatorApi } from "../fixtures/api";
import { expect } from "../fixtures/auth";
import { caretTo } from "../fixtures/editor";
import { orgTest as test } from "../fixtures/org";
import { uniqueName } from "../fixtures/seed";
import { expectAccessible, startInScheme, type ColourScheme } from "../fixtures/shell";
import { createPage, createSpace, deleteSpace, publishFromEditor, uniqueKey, type Space } from "../fixtures/spaces";

const editorBox = (page: Page) => page.locator("#page-body");
const bar = (page: Page) => page.getByRole("search", { name: "Find and replace" });
const status = (page: Page) => bar(page).getByRole("status");

const text = (value: string, marks?: string[]) => ({ type: "text", text: value, ...(marks ? { marks: marks.map((type) => ({ type })) } : {}) });

// Three times "draft" in any case, one of them split by a bold mark, and one
// "Draft" with a capital, so case and marks both change what is found.
const BODY = {
  type: "doc",
  content: [
    { type: "paragraph", content: [text("Draft one is the first.")] },
    { type: "paragraph", content: [text("Then a "), text("dra", ["bold"]), text("ft that runs across a mark.")] },
    { type: "paragraph", content: [text("The last draft goes here.")] },
  ],
};

test.describe("find and replace in the editor", { tag: ["@auth"] }, () => {
  const made: string[] = [];
  test.afterEach(async ({ api }) => {
    for (const key of made.splice(0)) await deleteSpace(api, key);
  });

  async function freshPage(api: StatorApi, testInfo: TestInfo, page: Page): Promise<Space> {
    const key = uniqueKey(testInfo);
    made.push(key);
    const space = await createSpace(api, key, uniqueName(testInfo, "Find"));
    const notes = await createPage(api, space.homePageId, "Notes", BODY);
    await expect(async () => {
      await page.goto(`/s/${space.key}/p/${notes.id}/notes/edit`);
      await expect(editorBox(page)).toContainText("The last draft", { timeout: 2_000 });
    }).toPass();
    return space;
  }

  test("Ctrl+F finds, steps, replaces one and all, undoes, and the page publishes", async ({ page, api }, testInfo) => {
    await freshPage(api, testInfo, page);

    // Outside the editor the key is the browser's.
    await page.getByRole("textbox", { name: "Title", exact: true }).focus();
    await page.keyboard.press("ControlOrMeta+f");
    await expect(bar(page)).toHaveCount(0);

    await caretTo(editorBox(page), "start");
    await page.keyboard.press("ControlOrMeta+f");
    const query = bar(page).getByRole("searchbox", { name: "Find" });
    await expect(query).toBeFocused();
    await page.keyboard.type("draft");
    await expect(status(page)).toHaveText("1 of 3 matches");
    await expect(editorBox(page).locator('[data-find-match="current"]')).toHaveText("Draft");
    await page.keyboard.press("Enter");
    await expect(status(page)).toHaveText("2 of 3 matches");
    await expect(editorBox(page).locator('[data-find-match="current"]')).toHaveText(["dra", "ft"]);
    await page.keyboard.press("Shift+Enter");
    await expect(status(page)).toHaveText("1 of 3 matches");

    await bar(page).getByRole("checkbox", { name: "Match case" }).check();
    await expect(status(page)).toHaveText("1 of 2 matches");
    await bar(page).getByRole("checkbox", { name: "Match case" }).uncheck();
    await expect(status(page)).toHaveText("1 of 3 matches");

    await bar(page).getByRole("textbox", { name: "Replace with" }).fill("plan");
    await bar(page).getByRole("button", { name: "Replace", exact: true }).click();
    await expect(editorBox(page).locator("p").first()).toHaveText("plan one is the first.");
    await expect(status(page)).toHaveText("1 of 2 matches");
    await bar(page).getByRole("button", { name: "Replace all" }).click();
    await expect(status(page)).toHaveText("Replaced 2 matches.");
    await expect(editorBox(page)).not.toContainText("draft");
    await expect(query).toBeFocused();

    await page.keyboard.press("Escape");
    await expect(bar(page)).toHaveCount(0);
    await expect(editorBox(page)).toBeFocused();
    await page.keyboard.press("ControlOrMeta+z");
    await expect(editorBox(page).locator("p").nth(1)).toHaveText("Then a draft that runs across a mark.");
    await expect(editorBox(page).locator("p").nth(2)).toHaveText("The last draft goes here.");
    await expect(editorBox(page).locator("p").first()).toHaveText("plan one is the first.");
    await page.keyboard.press("ControlOrMeta+Shift+z");
    await expect(editorBox(page)).not.toContainText("draft");

    await publishFromEditor(page);
    const doc = page.locator("[data-doc]");
    await expect(doc).toContainText("plan one is the first.");
    await expect(doc).toContainText("Then a plan that runs across a mark.");
    await expect(doc).toContainText("The last plan goes here.");
    await expect(doc.locator("[data-find-match]")).toHaveCount(0);
  });

  test("the bar is reached and left with the keyboard alone", { tag: "@desktop" }, async ({ page, api }, testInfo) => {
    await freshPage(api, testInfo, page);
    await caretTo(editorBox(page), "start");
    await page.keyboard.press("ControlOrMeta+f");
    await page.keyboard.type("Last");
    await page.keyboard.press("Tab");
    await page.keyboard.press("Tab");
    await expect(bar(page).getByRole("button", { name: "Next match" })).toBeFocused();
    await page.keyboard.press("Tab");
    await page.keyboard.press("Space");
    await expect(bar(page).getByRole("checkbox", { name: "Match case" })).toBeChecked();
    await expect(status(page)).toHaveText("No matches. Turn off Match case to find more.");
    await page.keyboard.press("Space");
    await page.keyboard.press("Tab");
    await page.keyboard.type("final");
    await page.keyboard.press("Enter");
    await expect(editorBox(page).locator("p").nth(2)).toHaveText("The final draft goes here.");
    await page.keyboard.press("ControlOrMeta+f");
    await expect(bar(page).getByRole("searchbox", { name: "Find" })).toBeFocused();
    await page.keyboard.press("Escape");
    await expect(editorBox(page)).toBeFocused();
  });

  for (const scheme of ["light", "dark"] as ColourScheme[]) {
    test(`the bar and its matches pass axe in ${scheme}`, async ({ page, api }, testInfo) => {
      await startInScheme(page, scheme);
      await freshPage(api, testInfo, page);
      await expect(page.locator("html")).toHaveAttribute("data-theme", scheme);
      await page.locator('[data-editor-action="find"]').click();
      await page.keyboard.type("draft");
      await expect(status(page)).toHaveText("1 of 3 matches");
      await expectAccessible(page);
      await bar(page).getByRole("checkbox", { name: "Match case" }).check();
      await expect(status(page)).toHaveText("1 of 2 matches");
      await expectAccessible(page);
    });
  }
});
