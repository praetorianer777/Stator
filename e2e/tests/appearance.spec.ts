import type { Page, TestInfo } from "@playwright/test";
import type { StatorApi } from "../fixtures/api";
import { expect } from "../fixtures/auth";
import { orgTest as test } from "../fixtures/org";
import { uniqueName } from "../fixtures/seed";
import { expectAccessible, startInScheme, type ColourScheme } from "../fixtures/shell";
import { createPage, createSpace, deleteSpace, uniqueKey, type Space } from "../fixtures/spaces";

const PIXEL_PNG = Buffer.from("iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mNkYPhfDwAChwGA60e6kgAAAABJRU5ErkJggg==", "base64");
const paragraph = (value: string) => ({ type: "paragraph", content: [{ type: "text", text: value }] });
// The standard column is 72rem, and this window is wide enough to show all of it beside the tree.
const STANDARD_WIDTH_REM = 72;
const WIDE_WINDOW_PX = 1800;
const article = (page: Page) => page.locator("article[data-page]");
const tree = (page: Page) => page.locator("[data-page-tree]");

/** Opens a page until it holds the words given, which a replica may lag behind on. */
async function open(page: Page, path: string, words: string): Promise<void> {
  await expect(async () => {
    await page.goto(path);
    await expect(page.locator("main")).toContainText(words, { timeout: 2_000 });
  }).toPass();
}

test.describe("page appearance", { tag: ["@auth"] }, () => {
  const made: string[] = [];
  test.afterEach(async ({ api }) => {
    for (const key of made.splice(0)) await deleteSpace(api, key);
  });

  async function freshSpace(api: StatorApi, testInfo: TestInfo, label: string): Promise<Space> {
    const key = uniqueKey(testInfo);
    made.push(key);
    return createSpace(api, key, uniqueName(testInfo, label));
  }

  async function chooseAppearance(page: Page) {
    await page.locator('[data-action="page-menu"]').click();
    await page.getByRole("menuitem", { name: "Appearance" }).click();
    const dialog = page.getByRole("dialog", { name: "Page appearance" });
    await dialog.getByLabel("Find an emoji").fill("rocket");
    await dialog.getByRole("button", { name: "Use rocket" }).click();
    await dialog.getByRole("radio", { name: /Full, for wide tables/ }).check();
    await dialog.locator("[data-cover-upload]").setInputFiles([{ name: "harbour.png", mimeType: "image/png", buffer: PIXEL_PNG }]);
    const focus = dialog.getByRole("button", { name: /Cover focus, 50% from the left/ });
    await expect(focus).toBeVisible();
    await focus.focus();
    await page.keyboard.press("ArrowRight");
    await page.keyboard.press("ArrowDown");
    await expect(dialog.locator("[data-cover-focus]")).toHaveAttribute("data-cover-focus", "55,55");
    await expectAccessible(page);
    await dialog.getByRole("button", { name: "Save" }).click();
    await expect(dialog).toHaveCount(0);
  }

  test("an editor gives a page an emoji, a full width and an uploaded cover, which stay", async ({ page, api }, testInfo) => {
    const space = await freshSpace(api, testInfo, "Looks");
    const plans = await createPage(api, space.homePageId, "Plans", { type: "doc", content: [paragraph("What we will do.")] });
    const path = `/s/${space.key}/p/${plans.id}/plans`;
    await open(page, path, "What we will do.");
    await chooseAppearance(page);

    await expect(page.locator("[data-page-icon]")).toHaveText("🚀");
    await expect(article(page)).toHaveAttribute("data-page-width", "full");
    const cover = article(page).locator("[data-page-cover] img");
    await expect(cover).toBeVisible();
    await expect(cover).toHaveCSS("object-position", "55% 55%");

    await page.reload();
    await expect(page.locator("[data-page-icon]")).toHaveText("🚀");
    await expect(article(page).locator("[data-page-cover] img")).toHaveCSS("object-position", "55% 55%");
    if (testInfo.project.name !== "mobile") {
      // The tree sits in a drawer on a phone; on a desk it is beside the page.
      await expect(tree(page).locator(`[data-tree-icon="🚀"]`)).toBeVisible();
    }
  });

  test("on a wide window a page with the standard width reads in a column of 72rem, in the reader and the editor", async ({ page, api }, testInfo) => {
    test.skip(testInfo.project.name === "mobile", "A phone is narrower than the column.");
    await page.setViewportSize({ width: WIDE_WINDOW_PX, height: 900 });
    const space = await freshSpace(api, testInfo, "Width");
    const notes = await createPage(api, space.homePageId, "Notes", { type: "doc", content: [paragraph("A line of notes.")] });
    const path = `/s/${space.key}/p/${notes.id}/notes`;
    await open(page, path, "A line of notes.");
    await expect(article(page)).toHaveAttribute("data-page-width", "fixed");
    const rootPx = await page.evaluate(() => parseFloat(getComputedStyle(document.documentElement).fontSize));
    expect((await article(page).boundingBox())!.width).toBe(STANDARD_WIDTH_REM * rootPx);
    await page.goto(`${path}/edit`);
    const editor = page.locator("form[data-page-editor]");
    await expect(editor).toBeVisible();
    expect((await editor.boundingBox())!.width).toBe(STANDARD_WIDTH_REM * rootPx);
  });

  for (const scheme of ["light", "dark"] as ColourScheme[]) {
    test(`a page with a cover and an emoji passes axe in ${scheme}`, async ({ page, api }, testInfo) => {
      const space = await freshSpace(api, testInfo, "Axe");
      const plans = await createPage(api, space.homePageId, "Plans", { type: "doc", content: [paragraph("Seen in a frame.")] });
      await startInScheme(page, scheme);
      await open(page, `/s/${space.key}/p/${plans.id}/plans`, "Seen in a frame.");
      await chooseAppearance(page);
      await expect(article(page).locator("[data-page-cover] img")).toBeVisible();
      await expectAccessible(page);
    });
  }
});
