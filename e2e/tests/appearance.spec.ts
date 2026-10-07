import type { Locator, Page, TestInfo } from "@playwright/test";
import type { StatorApi } from "../fixtures/api";
import { expect } from "../fixtures/auth";
import { orgTest as test } from "../fixtures/org";
import { openShowing } from "../fixtures/replica";
import { uniqueName } from "../fixtures/seed";
import { expectAccessible, startInScheme, type ColourScheme } from "../fixtures/shell";
import { createPage, createSpace, deleteSpace, uniqueKey, type Space } from "../fixtures/spaces";

const PIXEL_PNG = Buffer.from("iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mNkYPhfDwAChwGA60e6kgAAAABJRU5ErkJggg==", "base64");
const paragraph = (value: string) => ({ type: "paragraph", content: [{ type: "text", text: value }] });
// Text keeps a measure of 44rem while wide blocks reach 96rem, which this
// window has room for beside the tree; the phone is the narrowest we serve.
const MEASURE_REM = 44;
const MAX_WIDTH_REM = 96;
const WIDE_WINDOW_PX = 1920;
const DROP_ZONE_REACH_REM = 0.75;
const PHONE = { width: 360, height: 780 };
const table = (columns: number) => ({
  type: "table",
  content: [0, 1].map((r) => ({
    type: "tableRow",
    content: Array.from({ length: columns }, (_, c) => ({
      type: r === 0 ? "tableHeader" : "tableCell",
      content: [paragraph(r === 0 ? `Heading ${c + 1}` : `Cell ${c + 1}`)],
    })),
  })),
});
const diagram = { type: "diagram", attrs: { source: "flowchart LR\n  plan --> build --> ship" } };
const wideDoc = {
  type: "doc",
  content: [
    paragraph("A line of notes that is long enough to fill the measure of the page, and to run on past it if nothing held it back."),
    table(8),
    diagram,
    { type: "codeBlock", content: [{ type: "text", text: `const line = "${"x".repeat(200)}";` }] },
  ],
};
const article = (page: Page) => page.locator("article[data-page]");
const tree = (page: Page) => page.locator("[data-page-tree]");

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
    await dialog.getByRole("radio", { name: /Full, text as wide as the window/ }).check();
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
    await openShowing(page, path, "What we will do.");
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

  test("on a wide window text keeps its measure while a table and a diagram take the page's width, in the reader and the editor", async ({
    page,
    api,
  }, testInfo) => {
    test.skip(testInfo.project.name === "mobile", "A phone is narrower than the measure.");
    await page.setViewportSize({ width: WIDE_WINDOW_PX, height: 900 });
    const space = await freshSpace(api, testInfo, "Width");
    const notes = await createPage(api, space.homePageId, "Notes", wideDoc);
    const path = `/s/${space.key}/p/${notes.id}/notes`;
    await openShowing(page, path, "A line of notes");
    await expect(article(page)).toHaveAttribute("data-page-width", "fixed");
    const rootPx = await page.evaluate(() => parseFloat(getComputedStyle(document.documentElement).fontSize));
    const width = async (locator: Locator) => (await locator.boundingBox())!.width;

    const doc = page.locator("main [data-doc]");
    expect(await width(doc)).toBe(MAX_WIDTH_REM * rootPx);
    expect(await width(doc.locator("> p").first())).toBe(MEASURE_REM * rootPx);
    expect(await width(doc.locator("> .doc-table-wrap"))).toBe(MAX_WIDTH_REM * rootPx);
    expect(await width(doc.locator("> .doc-diagram"))).toBe(MAX_WIDTH_REM * rootPx);
    const textLeft = (await doc.locator("> p").first().boundingBox())!.x;
    expect((await page.locator("header.page-sheet-header").boundingBox())!.x).toBe(textLeft);
    expect(await page.locator("[data-comments]").boundingBox()).toMatchObject({ x: textLeft, width: MEASURE_REM * rootPx });
    // The attachments' drop zone reaches a little past the text on either side.
    const reach = DROP_ZONE_REACH_REM * rootPx;
    expect(await page.locator('section[aria-labelledby="attachments-title"]').boundingBox()).toMatchObject({
      x: textLeft - reach,
      width: MEASURE_REM * rootPx + 2 * reach,
    });

    await page.goto(`${path}/edit`);
    const body = page.locator("[data-editor]");
    await expect(body.locator("> p").first()).toContainText("A line of notes");
    await expect(body.locator("> .node-diagram")).toBeVisible();
    const inner = await body.evaluate((el) => {
      const style = getComputedStyle(el);
      return el.clientWidth - parseFloat(style.paddingLeft) - parseFloat(style.paddingRight);
    });
    expect(inner).toBeGreaterThan(MEASURE_REM * rootPx);
    expect(await width(body.locator("> p").first())).toBe(MEASURE_REM * rootPx);
    expect(await width(body.locator("> .tableWrapper"))).toBe(inner);
    expect(await width(body.locator("> .node-diagram"))).toBe(inner);
    expect((await body.locator("> p").first().boundingBox())!.x).toBe(textLeft);
  });

  test("on a phone a page with wide blocks does not scroll sideways, in the reader or the editor", async ({ page, api }, testInfo) => {
    await page.setViewportSize(PHONE);
    const space = await freshSpace(api, testInfo, "Phone");
    const notes = await createPage(api, space.homePageId, "Notes", wideDoc);
    const path = `/s/${space.key}/p/${notes.id}/notes`;
    const sideways = () => page.evaluate(() => [document.documentElement, document.querySelector("main")!].some((el) => el.scrollWidth > el.clientWidth));
    await openShowing(page, path, "A line of notes");
    await expect(page.locator("main [data-doc] > .doc-diagram")).toBeVisible();
    expect(await sideways()).toBe(false);
    await page.goto(`${path}/edit`);
    await expect(page.locator("[data-editor] > p").first()).toContainText("A line of notes");
    await expect(page.locator("[data-editor] > .node-diagram")).toBeVisible();
    expect(await sideways()).toBe(false);
  });

  for (const scheme of ["light", "dark"] as ColourScheme[]) {
    test(`a page with a cover and an emoji passes axe in ${scheme}`, async ({ page, api }, testInfo) => {
      const space = await freshSpace(api, testInfo, "Axe");
      const plans = await createPage(api, space.homePageId, "Plans", { type: "doc", content: [paragraph("Seen in a frame.")] });
      await startInScheme(page, scheme);
      await openShowing(page, `/s/${space.key}/p/${plans.id}/plans`, "Seen in a frame.");
      await chooseAppearance(page);
      await expect(article(page).locator("[data-page-cover] img")).toBeVisible();
      await expectAccessible(page);
    });
  }
});
