import type { Locator, Page, TestInfo } from "@playwright/test";
import { must, type StatorApi } from "../fixtures/api";
import { expect } from "../fixtures/auth";
import { png, upload } from "../fixtures/files";
import { orgTest as test } from "../fixtures/org";
import { openEditor, openShowing } from "../fixtures/replica";
import { uniqueName } from "../fixtures/seed";
import { expectAccessible, scrollsSideways, startInScheme, type ColourScheme } from "../fixtures/shell";
import { createPage, createSpace, deleteSpace, publishFromEditor, uniqueKey } from "../fixtures/spaces";

const PICTURE_WIDTH = 800;
const PICTURE_HEIGHT = 600;
const PICTURE_COLOUR: [number, number, number] = [30, 90, 160];

const dialog = (page: Page) => page.getByRole("dialog", { name: "Annotate screen.png" });
const canvas = (page: Page) => dialog(page).locator("[data-annotate-canvas]");
const tool = (page: Page, name: string) => dialog(page).locator(`[data-annotate-tool="${name}"]`);
const editorBox = (page: Page) => page.locator("#page-body");

/** Drags across the canvas between two points given as shares of its width and height. */
async function drag(page: Page, target: Locator, from: [number, number], to: [number, number]) {
  const box = await target.boundingBox();
  if (!box) throw new Error("The canvas is not on the screen.");
  await page.mouse.move(box.x + box.width * from[0], box.y + box.height * from[1]);
  await page.mouse.down();
  await page.mouse.move(box.x + box.width * to[0], box.y + box.height * to[1], { steps: 6 });
  await page.mouse.up();
}

/** The page of a login screen: a picture in its words, its file below. */
async function screenPage(api: StatorApi, testInfo: TestInfo) {
  const key = uniqueKey(testInfo);
  const space = await createSpace(api, key, uniqueName(testInfo, "Screens"));
  const target = await createPage(api, space.homePageId, "Login help");
  const original = png(PICTURE_WIDTH, PICTURE_HEIGHT, PICTURE_COLOUR);
  const shot = await upload(api, target.id, "screen.png", "image/png", original);
  must(
    await api.PATCH("/pages/{pageID}", {
      params: { path: { pageID: target.id } },
      body: {
        version: 1,
        body: {
          type: "doc",
          content: [
            { type: "paragraph", content: [{ type: "text", text: "The login screen." }] },
            { type: "image", attrs: { attachmentId: shot, alt: "The login screen", width: null } },
          ],
        },
      },
    }),
  );
  return { key, target, shot, original, path: `/s/${key}/p/${target.id}/login-help` };
}

async function filesOf(api: StatorApi, pageId: string) {
  return must(await api.GET("/pages/{pageID}/attachments", { params: { path: { pageID: pageId } } })).attachments;
}

/** How many of a picture's pixels are not the colour it was uploaded in, counted by the browser. */
async function pixelsDrawnOn(page: Page, id: string): Promise<number> {
  return page.evaluate(
    async ({ url, colour }) => {
      const bitmap = await createImageBitmap(await (await fetch(url, { credentials: "include" })).blob());
      const canvas = new OffscreenCanvas(bitmap.width, bitmap.height);
      const context = canvas.getContext("2d")!;
      context.drawImage(bitmap, 0, 0);
      const { data } = context.getImageData(0, 0, bitmap.width, bitmap.height);
      let other = 0;
      for (let i = 0; i < data.length; i += 4) if (data[i] !== colour[0] || data[i + 1] !== colour[1] || data[i + 2] !== colour[2]) other++;
      return other;
    },
    { url: `/api/v1/attachments/${id}?inline=1`, colour: PICTURE_COLOUR },
  );
}

test.describe("image annotation", { tag: ["@auth"] }, () => {
  const made: string[] = [];
  test.afterEach(async ({ api }) => {
    for (const key of made.splice(0)) await deleteSpace(api, key);
  });

  test("an author crops a picture in the editor, draws a box, an arrow and text, and the page shows the new version", async ({ page, api }, testInfo) => {
    test.slow();
    const { key, target, shot, original, path } = await screenPage(api, testInfo);
    made.push(key);
    await openEditor(page, path, "The login screen.");
    await editorBox(page).locator(`figure[data-image][data-attachment-id="${shot}"] img`).click();
    await page.locator('[data-editor-action="annotate-image"]').click();
    await expect(canvas(page)).toHaveAttribute("width", String(PICTURE_WIDTH));
    expect(await scrollsSideways(page)).toBe(false);

    await expect(tool(page, "box")).toHaveAttribute("aria-pressed", "true");
    await drag(page, canvas(page), [0.2, 0.2], [0.5, 0.45]);
    await expect(canvas(page)).toHaveAttribute("data-shapes", "1");
    await tool(page, "arrow").click();
    await dialog(page).getByRole("button", { name: "Yellow" }).click();
    await drag(page, canvas(page), [0.8, 0.8], [0.52, 0.47]);
    await expect(canvas(page)).toHaveAttribute("data-shapes", "2");
    await tool(page, "text").click();
    const box = await canvas(page).boundingBox();
    await page.mouse.click(box!.x + box!.width * 0.55, box!.y + box!.height * 0.7);
    await dialog(page).getByRole("textbox", { name: "Text" }).fill("Sign in here");
    await page.keyboard.press("Enter");
    await expect(canvas(page)).toHaveAttribute("data-shapes", "3");

    // A mistake, undone.
    await tool(page, "box").click();
    await drag(page, canvas(page), [0.6, 0.1], [0.7, 0.2]);
    await expect(canvas(page)).toHaveAttribute("data-shapes", "4");
    await dialog(page).getByRole("button", { name: "Undo" }).click();
    await expect(canvas(page)).toHaveAttribute("data-shapes", "3");

    await tool(page, "crop").click();
    await drag(page, canvas(page), [0.1, 0.1], [0.9, 0.85]);
    const crop = await canvas(page).getAttribute("data-crop");
    expect(crop).toMatch(/^\d+,\d+,\d+,\d+$/);
    const [, , cropWidth, cropHeight] = crop!.split(",").map(Number);
    expect(cropWidth).toBeLessThan(PICTURE_WIDTH);
    await expect(dialog(page).getByRole("checkbox", { name: "Show the edited picture in this page" })).toBeChecked();
    await expectAccessible(page);

    await dialog(page).getByRole("button", { name: "Save as new version" }).click();
    await expect(dialog(page)).toHaveCount(0);

    let edited = "";
    await expect(async () => {
      const files = await filesOf(api, target.id);
      expect(files).toHaveLength(2);
      const [latest] = files;
      expect(latest).toMatchObject({ fileName: "screen.png", contentType: "image/png", version: 2, editedFrom: 1, width: cropWidth, height: cropHeight });
      edited = latest!.id;
    }).toPass();
    const picture = editorBox(page).locator("figure[data-image]");
    await expect(picture).toHaveAttribute("data-attachment-id", edited);

    const before = await page.request.get(`/api/v1/attachments/${shot}`);
    expect(Buffer.compare(await before.body(), original)).toBe(0);
    const after = await page.request.get(`/api/v1/attachments/${edited}`);
    expect(Buffer.compare(await after.body(), original)).not.toBe(0);
    expect(await pixelsDrawnOn(page, edited)).toBeGreaterThan(0);

    await publishFromEditor(page);
    await expect(page.locator(`[data-doc] figure[data-image][data-attachment-id="${edited}"]`)).toBeVisible();
    const row = page.locator('[data-attachments] [data-attachment="screen.png"]');
    await expect(row).toHaveAttribute("data-version", "2");
    await expect(row).toContainText("Version 2, edited from version 1");
  });

  test("an author annotates a picture from the files below the page, and the page keeps the version it shows", async ({ page, api }, testInfo) => {
    test.slow();
    const { key, target, shot, path } = await screenPage(api, testInfo);
    made.push(key);
    const row = page.locator('[data-attachments] [data-attachment="screen.png"]');
    await openShowing(page, path, row);
    await row.getByRole("button", { name: "Annotate screen.png" }).click();
    await expect(canvas(page)).toHaveAttribute("width", String(PICTURE_WIDTH));
    await expect(dialog(page).getByRole("checkbox")).toHaveCount(0);

    // By keyboard alone: a box in the middle, moved and grown, then saved.
    await dialog(page).getByRole("application").focus();
    await page.keyboard.press("Enter");
    await page.keyboard.press("ArrowRight");
    await page.keyboard.press("Shift+ArrowDown");
    await expect(canvas(page)).toHaveAttribute("data-shapes", "1");
    await expect(dialog(page).locator("[data-annotate-status]")).toHaveText("Box selected.");
    await dialog(page).getByRole("button", { name: "Save as new version" }).click();
    await expect(dialog(page)).toHaveCount(0);
    await expect(page.locator("[data-attachment-notice]")).toHaveText("Saved the edited screen.png as version 2.");
    await expect(row).toHaveAttribute("data-version", "2");
    await expect(page.locator(`[data-doc] figure[data-image][data-attachment-id="${shot}"]`)).toBeVisible();
    const files = await filesOf(api, target.id);
    expect(files.map((f) => [f.version, f.editedFrom, f.width, f.height])).toEqual([
      [2, 1, PICTURE_WIDTH, PICTURE_HEIGHT],
      [1, null, PICTURE_WIDTH, PICTURE_HEIGHT],
    ]);
  });

  for (const scheme of ["light", "dark"] as ColourScheme[]) {
    test(`the annotation editor passes axe in ${scheme} and fits the screen`, async ({ page, api }, testInfo) => {
      await startInScheme(page, scheme);
      const { key, path } = await screenPage(api, testInfo);
      made.push(key);
      const row = page.locator('[data-attachments] [data-attachment="screen.png"]');
      await openShowing(page, path, row);
      await row.getByRole("button", { name: "Annotate screen.png" }).click();
      await expect(canvas(page)).toBeVisible();
      await dialog(page).getByRole("application").focus();
      await page.keyboard.press("Enter");
      await tool(page, "text").click();
      await dialog(page).getByRole("application").focus();
      await page.keyboard.press("Enter");
      await expect(dialog(page).getByRole("textbox", { name: "Text" })).toBeFocused();
      await expectAccessible(page);
      expect(await scrollsSideways(page)).toBe(false);
      await expect(dialog(page).getByRole("button", { name: "Save as new version" })).toBeInViewport();
      const box = await canvas(page).boundingBox();
      expect(box!.width).toBeLessThanOrEqual(page.viewportSize()!.width);

      page.once("dialog", (asked) => void asked.accept());
      await dialog(page).getByRole("button", { name: "Cancel" }).click();
      await expect(dialog(page)).toHaveCount(0);
    });
  }
});
