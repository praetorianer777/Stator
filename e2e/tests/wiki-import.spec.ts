import { writeFileSync } from "node:fs";
import type { Page, TestInfo } from "@playwright/test";
import { must } from "../fixtures/api";
import { expect } from "../fixtures/auth";
import { png } from "../fixtures/files";
import { orgTest as test } from "../fixtures/org";
import { ONE_LOOK, openPage } from "../fixtures/replica";
import { expectAccessible, scrollsSideways, startInScheme, type ColourScheme } from "../fixtures/shell";
import { deleteSpace, uniqueKey } from "../fixtures/spaces";
import { zip } from "../fixtures/zip";

// The worker runs an import as a job of its own, which on a stack the whole
// suite keeps busy takes a while to be begun and done.
const WORKED = { timeout: 60_000 } as const;

/** A page file of an HTML space export, in the structure docs/wiki-import.md describes. */
function pageFile(title: string, content: string, extra = ""): string {
  return `<!DOCTYPE html><html><head><title>Field Notes : ${title}</title></head><body>
<h1><span id="title-text">Field Notes : ${title}</span></h1>
<div class="page-metadata">Created by <span class="author">Pat Unknown</span> on <time datetime="2026-03-04">Mar 04, 2026</time></div>
<div id="main-content">${content}</div>${extra}</body></html>`;
}

/** An HTML export of a nested tree, a picture, a label and a comment, and a frame the import leaves out. */
function htmlExport(): Buffer {
  return zip({
    "notes/index.html": `<html><head><title>Field Notes</title></head><body><ul><li><a href="Home_1.html">Home</a><ul>
      <li><a href="Guide_2.html">Guide</a><ul><li><a href="Setup_3.html">Setup</a></li></ul></li></ul></li></ul></body></html>`,
    "notes/Home_1.html": pageFile("Home", `<p>Start with the <a href="Guide_2.html">guide</a>.</p>`),
    "notes/Guide_2.html": pageFile(
      "Guide",
      `<p>The overview first.</p><p><img src="attachments/2/overview.png" alt="The overview"></p>
       <div class="callout callout-warning"><p>Mind the steps.</p></div>`,
      `<div class="labels"><ul><li><a href="labels/how-to.html">How To</a></li></ul></div>
       <div id="comments-section"><div class="comment"><span class="author">Pat Unknown</span><time datetime="2026-03-05T10:00:00Z">Mar 5</time>
       <div class="comment-body"><p>Clear and short.</p></div></div></div>`,
    ),
    "notes/Setup_3.html": pageFile("Setup", `<p>Install it.</p><iframe src="https://video.example.com/1"></iframe>`),
    "notes/attachments/2/overview.png": png(40, 30, [40, 120, 200]),
  });
}

/** Uploads the export under the key and waits for the worker's report. */
async function importExport(page: Page, testInfo: TestInfo, key: string) {
  const saved = testInfo.outputPath("field-notes-html.zip");
  writeFileSync(saved, htmlExport());
  await page.goto("/spaces/import");
  await page.locator("[data-import-file]").setInputFiles(saved);
  await page.getByLabel("Key", { exact: true }).fill(key);
  await page.locator('[data-action="confirm-import-space"]').click();
  await expect(page.locator("[data-import-done]")).toBeVisible(WORKED);
  const report = page.locator("[data-import-report]");
  await expect(report).toContainText("3 pages with 3 versions, 1 files and 1 comments came across.");
  await expect(report).toContainText("An HTML export holds no history");
  await expect(report.locator('[data-lost-kind="embed"]')).toHaveText("Setup: Embedded content (iframe) was left out.");
  await expect(report.locator("[data-import-people]")).toContainText("Pat Unknown");
  expect(await scrollsSideways(page)).toBe(false);
}

test.describe("importing another wiki's export", { tag: ["@auth"] }, () => {
  test("an administrator imports an HTML export as a new space and reads its tree, picture and comment", async ({ page, api }, testInfo) => {
    test.slow();
    const key = uniqueKey(testInfo);
    try {
      await importExport(page, testInfo, key);
      await expectAccessible(page);
      await page.locator('[data-action="open-imported"]').click();
      await expect(page).toHaveURL(new RegExp(`/s/${key}`));

      const { pages } = must(await api.GET("/spaces/{spaceKey}/outline", { params: { path: { spaceKey: key } } }));
      expect(pages.map((p) => `${"  ".repeat(p.depth)}${p.title}`)).toEqual(["Home", "  Guide", "    Setup"]);
      const guide = pages.find((p) => p.title === "Guide")!;
      const setup = pages.find((p) => p.title === "Setup")!;

      const picture = page.locator('main img[alt="The overview"]');
      await openPage(page, key, guide, () => expect(picture).toBeVisible(ONE_LOOK));
      await expect.poll(() => picture.evaluate((img: HTMLImageElement) => img.naturalWidth)).toBe(40);
      await expect(page.locator("main")).toContainText("Mind the steps.");
      await expect(page.locator('[data-page-labels] [data-label="how-to"]')).toBeVisible();
      await expect(page.locator("[data-comment-author]")).toHaveText(/./);
      await expect(page.locator("#comments")).toContainText("Clear and short.");
      await expect(page.locator("[data-comment-original]")).toContainText("Pat Unknown");
      await expectAccessible(page);

      await openPage(page, key, setup);
      await expect(page.getByRole("navigation", { name: "Breadcrumb" }).getByRole("link", { name: "Guide" })).toBeVisible();
      await expect(page.locator("main")).toContainText("Install it.");
    } finally {
      await deleteSpace(api, key);
    }
  });

  test("the report of an export is accessible in the dark scheme", async ({ page, api }, testInfo) => {
    test.slow();
    const key = uniqueKey(testInfo);
    const scheme: ColourScheme = "dark";
    try {
      await startInScheme(page, scheme);
      await importExport(page, testInfo, key);
      await expectAccessible(page);
    } finally {
      await deleteSpace(api, key);
    }
  });
});
