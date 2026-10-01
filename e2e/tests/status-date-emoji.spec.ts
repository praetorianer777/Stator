import type { Page, TestInfo } from "@playwright/test";
import type { StatorApi } from "../fixtures/api";
import { expect } from "../fixtures/auth";
import { caretTo } from "../fixtures/editor";
import { orgTest as test } from "../fixtures/org";
import { uniqueName } from "../fixtures/seed";
import { expectAccessible, startInScheme, type ColourScheme } from "../fixtures/shell";
import { createPage, createSpace, deleteSpace, publishFromEditor, uniqueKey, type Space } from "../fixtures/spaces";

// Dates are shown in the reader's own format; a British browser proves it is
// not the author's, nor a fixed one. A German one would turn the whole
// interface German, which language.spec.ts covers.
const LOCALE = "en-GB";
const DAY = "2026-11-02";
const DAY_SHOWN = "2 Nov 2026";

const editorBox = (page: Page) => page.locator("#page-body");
const readView = (page: Page) => page.locator("[data-doc]");

async function insert(page: Page, query: string): Promise<void> {
  await page.keyboard.type(`/${query}`);
  await expect(page.getByRole("listbox", { name: "Insert a block" })).toBeVisible();
  await page.keyboard.press("Enter");
}

/** Opens a page's editor, waiting out a replica that has not seen the page yet. */
async function openEditor(page: Page, path: string, text: string): Promise<void> {
  await expect(async () => {
    await page.goto(`${path}/edit`);
    await expect(editorBox(page)).toContainText(text, { timeout: 2_000 });
  }).toPass();
}

test.use({ locale: LOCALE });

test.describe("status labels, dates and emoji", { tag: ["@auth", "@desktop"] }, () => {
  const made: string[] = [];
  test.afterEach(async ({ api }) => {
    for (const key of made.splice(0)) await deleteSpace(api, key);
  });

  async function freshSpace(api: StatorApi, testInfo: TestInfo, label: string): Promise<Space> {
    const key = uniqueKey(testInfo);
    made.push(key);
    return createSpace(api, key, uniqueName(testInfo, label));
  }

  test("go in and change from the keyboard, and read the same after saving and reloading", async ({ page, api }, testInfo) => {
    const space = await freshSpace(api, testInfo, "Values");
    const release = await createPage(api, space.homePageId, "Release", {
      type: "doc",
      content: [{ type: "paragraph", content: [{ type: "text", text: "Release" }] }],
    });
    const path = `/s/${space.key}/p/${release.id}/release`;
    await openEditor(page, path, "Release");
    await caretTo(editorBox(page), "end");
    await page.keyboard.type(" ");

    await insert(page, "status");
    const statusDialog = page.getByRole("dialog", { name: "Status" });
    const words = statusDialog.getByLabel("Words");
    await expect(words).toBeFocused();
    await expect(words).toHaveValue("To do");
    await page.keyboard.press("ControlOrMeta+A");
    await page.keyboard.type("In review");
    // The colours are one radio group: one Tab in, the arrows to move.
    await page.keyboard.press("Tab");
    await expect(statusDialog.getByRole("radio", { name: "Grey" })).toBeFocused();
    for (let i = 0; i < 3; i++) await page.keyboard.press("ArrowRight");
    await expect(statusDialog.getByRole("radio", { name: "Yellow" })).toBeChecked();
    await page.keyboard.press("Tab");
    await page.keyboard.press("Enter");
    await expect(statusDialog).toBeHidden();
    const status = editorBox(page).locator(".doc-status");
    await expect(status).toHaveText("Status In review");
    await expect(status).toHaveAttribute("data-status-label", "warning");
    await expect(editorBox(page)).toBeFocused();
    // The caret is put back at the end as a person would, since focus that
    // comes back from a dialog may still be settling when the next key lands.
    await caretTo(editorBox(page), "end");

    // With the caret after the status, the left arrow selects it, and Enter opens it again.
    await page.keyboard.press("ArrowLeft");
    await expect(editorBox(page).locator(".ProseMirror-selectednode .doc-status")).toHaveCount(1);
    await page.keyboard.press("Enter");
    await expect(statusDialog).toBeVisible();
    await expect(words).toHaveValue("In review");
    await page.keyboard.press("Tab");
    await expect(statusDialog.getByRole("radio", { name: "Yellow" })).toBeFocused();
    await page.keyboard.press("ArrowLeft");
    await expect(statusDialog.getByRole("radio", { name: "Green" })).toBeChecked();
    await page.keyboard.press("Tab");
    await page.keyboard.press("Enter");
    await expect(statusDialog).toBeHidden();
    await expect(status).toHaveAttribute("data-status-label", "success");
    await expect(editorBox(page)).toBeFocused();
    // The caret is put back at the end as a person would, since focus that
    // comes back from a dialog may still be settling when the next key lands.
    await caretTo(editorBox(page), "end");

    await page.keyboard.type(" by ");
    await insert(page, "date");
    const dateDialog = page.getByRole("dialog", { name: "Date" });
    const day = dateDialog.getByLabel("Day");
    await expect(day).toBeFocused();
    await day.fill(DAY);
    await page.keyboard.press("Enter");
    await expect(dateDialog).toBeHidden();
    const date = editorBox(page).locator("time");
    await expect(date).toHaveAttribute("datetime", DAY);
    await expect(date).toHaveText(DAY_SHOWN);
    await expect(editorBox(page)).toBeFocused();
    // The caret is put back at the end as a person would, since focus that
    // comes back from a dialog may still be settling when the next key lands.
    await caretTo(editorBox(page), "end");

    await page.keyboard.type(" :tad");
    const emoji = page.getByRole("listbox", { name: "Emoji" });
    await expect(emoji.getByRole("option").first()).toContainText(":tada:");
    await expect(emoji.getByRole("option").first()).toHaveAttribute("aria-selected", "true");
    await page.keyboard.press("Enter");
    await expect(emoji).toBeHidden();
    await expect(editorBox(page).locator("p").first()).toContainText("🎉");

    await expect(page.locator("[data-draft-status]")).toHaveAttribute("data-draft-status", "saved");
    await page.reload();
    await expect(editorBox(page).locator(".doc-status")).toHaveText("Status In review");
    await expect(editorBox(page).locator("time")).toHaveText(DAY_SHOWN);
    await publishFromEditor(page);

    for (const pass of ["published", "reloaded"]) {
      if (pass === "reloaded") await page.reload();
      const shown = readView(page).locator(".doc-status");
      await expect(shown, pass).toHaveText("Status In review");
      await expect(shown, pass).toHaveAttribute("data-status-label", "success");
      await expect(readView(page).locator(`time[datetime="${DAY}"]`), pass).toHaveText(DAY_SHOWN);
      await expect(readView(page).locator("p").first(), pass).toHaveText(`Release Status In review by ${DAY_SHOWN} 🎉`);
    }
  });

  const COLOURS = ["neutral", "accent", "success", "warning", "danger"];

  /** A page with a date and a status of every colour, opened in the given palette; returns its path. */
  async function board(page: Page, api: StatorApi, testInfo: TestInfo, scheme: ColourScheme): Promise<string> {
    const space = await freshSpace(api, testInfo, "Axe");
    const made = await createPage(api, space.homePageId, "Board", {
      type: "doc",
      content: [
        {
          type: "paragraph",
          content: [
            { type: "text", text: "Due " },
            { type: "date", attrs: { date: DAY } },
            ...COLOURS.flatMap((color) => [
              { type: "text", text: " " },
              { type: "status", attrs: { label: `State ${color}`, color } },
            ]),
          ],
        },
      ],
    });
    await startInScheme(page, scheme);
    return `/s/${space.key}/p/${made.id}/board`;
  }

  async function openBoardEditor(page: Page, path: string, scheme: ColourScheme): Promise<void> {
    await openEditor(page, path, "State danger");
    await expect(editorBox(page).locator(".doc-status")).toHaveCount(COLOURS.length);
    await expect(page.locator("html")).toHaveAttribute("data-theme", scheme);
  }

  // A scan of the whole page takes about a second, and five in one test went
  // past the timeout on a busy machine, so each test holds at most two.
  for (const scheme of ["light", "dark"] as ColourScheme[]) {
    test(`every status colour and a date pass axe in ${scheme} when read`, async ({ page, api }, testInfo) => {
      const path = await board(page, api, testInfo, scheme);
      await expect(async () => {
        await page.goto(path);
        await expect(readView(page).locator(".doc-status")).toHaveCount(COLOURS.length, { timeout: 1_000 });
      }).toPass();
      await expect(page.locator("html")).toHaveAttribute("data-theme", scheme);
      await expectAccessible(page);
    });

    test(`every status colour and a date pass axe in ${scheme} in the editor, and so does the emoji list`, async ({ page, api }, testInfo) => {
      await openBoardEditor(page, await board(page, api, testInfo, scheme), scheme);
      await expectAccessible(page);
      await caretTo(editorBox(page), "end");
      await page.keyboard.type(" :");
      await expect(page.getByRole("listbox", { name: "Emoji" }).getByRole("option")).toHaveCount(8);
      await expectAccessible(page);
    });

    test(`the status and date dialogs pass axe in ${scheme}`, async ({ page, api }, testInfo) => {
      await openBoardEditor(page, await board(page, api, testInfo, scheme), scheme);
      await editorBox(page).locator(".doc-status").first().click();
      await expect(page.getByRole("dialog", { name: "Status" })).toBeVisible();
      await expectAccessible(page);
      await page.keyboard.press("Escape");
      await editorBox(page).locator("time").click();
      await expect(page.getByRole("dialog", { name: "Date" })).toBeVisible();
      await expectAccessible(page);
    });
  }
});
