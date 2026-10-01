import { mkdir, writeFile } from "node:fs/promises";
import { dirname, join } from "node:path";
import type { Download, Page, TestInfo } from "@playwright/test";
import { expect } from "../fixtures/auth";
import type { StatorApi } from "../fixtures/api";
import { orgTest as test } from "../fixtures/org";
import { uniqueName } from "../fixtures/seed";
import { expectAccessible, startInScheme, type ColourScheme } from "../fixtures/shell";
import { childTitles, createPage, createSpace, deleteSpace, uniqueKey, type Space } from "../fixtures/spaces";

// One pixel, as small as a PNG gets, so a picture costs the suite nothing.
const PIXEL_PNG = Buffer.from("iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mNkYPhfDwAChwGA60e6kgAAAABJRU5ErkJggg==", "base64");

const importDialog = (page: Page) => page.locator("[data-markdown-import]");
const exportDialog = (page: Page) => page.locator("[data-markdown-export]");

/** Writes a folder to the test's own directory, as a person would have it on disk. */
async function folder(testInfo: TestInfo, name: string, files: Record<string, string | Buffer>): Promise<string> {
  const root = testInfo.outputPath(name);
  for (const [path, content] of Object.entries(files)) {
    const file = join(root, path);
    await mkdir(dirname(file), { recursive: true });
    await writeFile(file, content);
  }
  return root;
}

async function bytesOf(download: Download): Promise<Buffer> {
  const chunks: Buffer[] = [];
  for await (const chunk of await download.createReadStream()) chunks.push(chunk as Buffer);
  return Buffer.concat(chunks);
}

async function openPageMenuItem(page: Page, action: string) {
  await page.locator('[data-action="page-menu"]').click();
  await page.locator(`[data-action="${action}"]`).click();
}

test.describe("Markdown import and export", { tag: ["@auth"] }, () => {
  const made: string[] = [];
  test.afterEach(async ({ api }) => {
    for (const key of made.splice(0)) await deleteSpace(api, key);
  });

  async function aSpace(api: StatorApi, testInfo: TestInfo): Promise<Space> {
    const key = uniqueKey(testInfo);
    made.push(key);
    return createSpace(api, key, uniqueName(testInfo, "Docs"));
  }

  test("a folder imports into a space, and the pages export with their files", async ({ page, api }, testInfo) => {
    const space = await aSpace(api, testInfo);
    const root = await folder(testInfo, "handbook", {
      "README.md": "# Handbook\n\nStart with [setting up](setup.md).\n\n![A pixel](pixel.png)\n\n> [!TIP]\n> Read it twice.\n",
      "setup.md": "# Setting up\n\n- [x] Install\n- [ ] Configure\n",
      "pixel.png": PIXEL_PNG,
    });

    await page.goto(`/s/${space.key}`);
    await openPageMenuItem(page, "import-markdown");
    await importDialog(page).locator("[data-markdown-folder]").setInputFiles(root);
    await expect(importDialog(page)).toContainText("3 files chosen");
    await importDialog(page).locator('[data-action="confirm-import"]').click();
    await expect(importDialog(page).locator("[data-import-result]")).toHaveText("Imported 2 pages.");
    await importDialog(page).getByRole("link", { name: "Handbook" }).click();

    await expect(page.locator("[data-page-title]")).toHaveText("Handbook");
    const doc = page.locator("[data-doc]");
    await expect(doc.getByRole("img", { name: "A pixel" })).toBeVisible();
    await expect(doc.getByRole("note")).toContainText("Read it twice.");
    await doc.getByRole("link", { name: "setting up" }).click();
    await expect(page.locator("[data-page-title]")).toHaveText("Setting up");
    await expect(page.locator("[data-doc] input[type=checkbox]")).toHaveCount(2);
    await page.goBack();
    await expect(page.locator("[data-page-title]")).toHaveText("Handbook");

    await openPageMenuItem(page, "export-markdown");
    await exportDialog(page).locator('[data-export-scope="subtree"]').check();
    const downloading = page.waitForEvent("download");
    await exportDialog(page).locator('[data-action="download-markdown"]').click();
    const archive = await downloading;
    await expect(exportDialog(page)).toHaveCount(0);
    expect(archive.suggestedFilename()).toBe("handbook.zip");
    const bytes = (await bytesOf(archive)).toString("latin1");
    for (const name of ["handbook.md", "handbook.files/pixel.png", "handbook/setting-up.md"]) expect(bytes).toContain(name);

    await openPageMenuItem(page, "export-markdown");
    await exportDialog(page).locator('[data-export-scope="markdown"]').check();
    const single = page.waitForEvent("download");
    await exportDialog(page).locator('[data-action="download-markdown"]').click();
    const file = await single;
    expect(file.suggestedFilename()).toBe("handbook.md");
    const markdown = (await bytesOf(file)).toString("utf8");
    expect(markdown).toContain("# Handbook\n");
    expect(markdown).toContain("![A pixel](handbook.files/pixel.png)");
    expect(markdown).toContain("> [!TIP]\n> Read it twice.");
  });

  test("an import runs from the keyboard alone", async ({ page, api }, testInfo) => {
    const space = await aSpace(api, testInfo);
    await page.goto(`/s/${space.key}`);
    const trigger = page.locator('[data-action="page-menu"]');
    await trigger.focus();
    await page.keyboard.press("Enter");
    const item = page.getByRole("menuitem", { name: "Import Markdown" });
    while (!(await item.evaluate((el) => el === document.activeElement))) await page.keyboard.press("ArrowDown");
    await page.keyboard.press("Enter");
    await expect(importDialog(page)).toBeVisible();

    const choose = importDialog(page).locator('[data-action="choose-markdown-files"]');
    while (!(await choose.evaluate((el) => el === document.activeElement))) await page.keyboard.press("Tab");
    const chooser = page.waitForEvent("filechooser");
    await page.keyboard.press("Enter");
    await (await chooser).setFiles([{ name: "notes.md", mimeType: "text/markdown", buffer: Buffer.from("# Meeting notes\n\nDecided **nothing**.\n") }]);
    await expect(importDialog(page)).toContainText("1 file chosen");
    const submit = importDialog(page).locator('[data-action="confirm-import"]');
    while (!(await submit.evaluate((el) => el === document.activeElement))) await page.keyboard.press("Tab");
    await page.keyboard.press("Enter");
    await expect(importDialog(page).locator("[data-import-result]")).toHaveText("Imported 1 page.");
    await page.keyboard.press("Escape");
    await expect(importDialog(page)).toHaveCount(0);
    await expect(trigger).toBeFocused();
    expect(await childTitles(api, space.key, space.homePageId)).toContain("Meeting notes");
  });

  for (const scheme of ["light", "dark"] as ColourScheme[]) {
    test(`the export and import dialogs pass axe in ${scheme}`, async ({ page, api }, testInfo) => {
      await startInScheme(page, scheme);
      const space = await aSpace(api, testInfo);
      const guide = await createPage(api, space.homePageId, "Guide");
      await page.goto(`/s/${space.key}/p/${guide.id}/guide`);
      await openPageMenuItem(page, "export-markdown");
      await expect(exportDialog(page)).toBeVisible();
      await expectAccessible(page);
      await page.keyboard.press("Escape");

      await openPageMenuItem(page, "import-markdown");
      await importDialog(page)
        .locator("[data-markdown-files]")
        .setInputFiles([{ name: "logo.png", mimeType: "image/png", buffer: PIXEL_PNG }]);
      await importDialog(page).locator('[data-action="confirm-import"]').click();
      await expect(importDialog(page).getByRole("alert")).toContainText("None of these is a Markdown file.");
      await expectAccessible(page);

      await importDialog(page)
        .locator("[data-markdown-files]")
        .setInputFiles([{ name: "broken.md", mimeType: "text/markdown", buffer: Buffer.from(`${"> ".repeat(60)}deep\n`) }]);
      await importDialog(page).locator('[data-action="confirm-import"]').click();
      await expect(importDialog(page).getByRole("alert")).toContainText("broken.md");
      await expectAccessible(page);
    });
  }
});
