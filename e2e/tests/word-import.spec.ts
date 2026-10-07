import type { Page } from "@playwright/test";
import { expect } from "../fixtures/auth";
import { bullet, DOCX_TYPE, para, picture, table, wordDocument } from "../fixtures/docx";
import { png } from "../fixtures/files";
import { orgTest as test } from "../fixtures/org";
import { ONE_LOOK, openUntil } from "../fixtures/replica";
import { expectAccessible, scrollsSideways, startInScheme, type ColourScheme } from "../fixtures/shell";
import { childTitles, createPage, createSpace, deleteSpace, uniqueKey } from "../fixtures/spaces";

/** The worker begins an import within a second in the stack, then makes a page or two a second on a busy runner. */
const IMPORTED = { timeout: 60_000 } as const;

const heading = (page: Page) => page.locator("main").getByRole("heading", { level: 1 });
const importDialog = (page: Page) => page.locator("[data-word-import-dialog]");
const openTitled = (page: Page, path: string, title: string) => openUntil(page, path, () => expect(heading(page)).toHaveText(title, ONE_LOOK));

async function openImport(page: Page) {
  await page.locator('[data-action="page-menu"]').click();
  await page.locator('[data-action="import-word"]').click();
  await expect(importDialog(page)).toBeVisible();
}

/** A release guide as an author would have written it in Word. */
function guideDocument(title: string) {
  return wordDocument({
    title,
    png: png(48, 24, [200, 60, 40]),
    body: [
      para("Before you start", "Heading1"),
      para("Check the steps below."),
      bullet("Build the release"),
      bullet("Run the tests", 1),
      bullet("Tag it"),
      table([
        ["Step", "Owner"],
        ["Build", "Ada"],
      ]),
      picture("The release train", 48, 24),
    ].join(""),
  });
}

test.describe("Word import", { tag: ["@auth"] }, () => {
  test("an author imports a document under a page and sees its headings, list, table and picture", async ({ page, api }, testInfo) => {
    test.slow();
    const key = uniqueKey(testInfo);
    const space = await createSpace(api, key, "Imported handbook");
    try {
      const parent = await createPage(api, space.homePageId, `Releases ${key}`);
      await openTitled(page, `/s/${key}/p/${parent.id}/releases`, parent.title);
      await openImport(page);
      await importDialog(page)
        .locator("[data-word-files]")
        .setInputFiles([{ name: "release-guide.docx", mimeType: DOCX_TYPE, buffer: guideDocument(`Release guide ${key}`) }]);
      await expect(importDialog(page)).toContainText("1 file chosen");
      await importDialog(page).locator('[data-action="confirm-word-import"]').click();
      const made = importDialog(page).getByRole("link", { name: `Release guide ${key}` });
      await expect(made).toBeVisible();
      await made.click();

      await expect(heading(page)).toHaveText(`Release guide ${key}`);
      const doc = page.locator("[data-doc]");
      await expect(doc.getByRole("heading", { name: "Before you start" })).toBeVisible();
      await expect(doc.getByRole("listitem").filter({ hasText: "Build the release" })).toBeVisible();
      await expect(doc.locator("ul ul li")).toHaveText("Run the tests");
      await expect(doc.getByRole("columnheader", { name: "Owner" })).toBeVisible();
      await expect(doc.getByRole("cell", { name: "Ada" })).toBeVisible();
      await expect(doc.getByRole("img", { name: "The release train" })).toBeVisible();
      expect(await scrollsSideways(page)).toBe(false);
    } finally {
      await deleteSpace(api, key);
    }
  });

  test("three documents at once are imported by the worker, with a report for each", async ({ page, api }, testInfo) => {
    test.slow();
    const key = uniqueKey(testInfo);
    const space = await createSpace(api, key, "Imported minutes");
    try {
      const parent = await createPage(api, space.homePageId, `Minutes ${key}`);
      await openTitled(page, `/s/${key}/p/${parent.id}/minutes`, parent.title);
      await openImport(page);
      const minutes = ["January", "February", "March"].map((month) => ({
        name: `${month.toLowerCase()}.docx`,
        mimeType: DOCX_TYPE,
        buffer: wordDocument({ title: `${month} minutes`, body: para(`What we decided in ${month}.`) }),
      }));
      await importDialog(page).locator("[data-word-files]").setInputFiles(minutes);
      await expect(importDialog(page)).toContainText("3 files chosen");
      await importDialog(page).locator('[data-action="confirm-word-import"]').click();
      await expect(importDialog(page).locator("[data-import-result]")).toHaveText("Imported 3 pages.", IMPORTED);
      for (const month of ["January", "February", "March"]) {
        await expect(importDialog(page).getByRole("link", { name: `${month} minutes` })).toBeVisible();
      }
      // The worker wrote the pages, so a read of them may reach a replica behind it.
      await expect.poll(async () => (await childTitles(api, key, parent.id)).sort()).toEqual(["February minutes", "January minutes", "March minutes"]);
      await importDialog(page).getByRole("link", { name: "February minutes" }).click();
      await expect(heading(page)).toHaveText("February minutes");
      await expect(page.locator("[data-doc]")).toContainText("What we decided in February.");
    } finally {
      await deleteSpace(api, key);
    }
  });

  for (const scheme of ["light", "dark"] as ColourScheme[]) {
    test(`the import dialog passes axe in ${scheme}, refused and done`, async ({ page, api }, testInfo) => {
      test.slow();
      await startInScheme(page, scheme);
      const key = uniqueKey(testInfo);
      const space = await createSpace(api, key, "Accessible imports");
      try {
        const parent = await createPage(api, space.homePageId, `Inbox ${key}`);
        await openTitled(page, `/s/${key}/p/${parent.id}/inbox`, parent.title);
        await openImport(page);
        await expectAccessible(page);

        await importDialog(page)
          .locator("[data-word-files]")
          .setInputFiles([{ name: "notes.txt", mimeType: "text/plain", buffer: Buffer.from("plain words") }]);
        await importDialog(page).locator('[data-action="confirm-word-import"]').click();
        await expect(importDialog(page).getByRole("alert")).toContainText("None of these is a Word document.");
        await expectAccessible(page);

        await importDialog(page)
          .locator("[data-word-files]")
          .setInputFiles([{ name: "broken.docx", mimeType: DOCX_TYPE, buffer: Buffer.from("not a document") }]);
        await importDialog(page).locator('[data-action="confirm-word-import"]').click();
        await expect(importDialog(page).getByRole("alert")).toContainText("broken.docx cannot be a page");
        await expectAccessible(page);

        await importDialog(page)
          .locator("[data-word-files]")
          .setInputFiles([{ name: "guide.docx", mimeType: DOCX_TYPE, buffer: guideDocument(`Guide ${key}`) }]);
        await importDialog(page).locator('[data-action="confirm-word-import"]').click();
        await expect(importDialog(page).getByRole("link", { name: `Guide ${key}` })).toBeVisible();
        await expectAccessible(page);
        await page.keyboard.press("Escape");
        await expect(importDialog(page)).toHaveCount(0);
        await expect(page.locator('[data-action="page-menu"]')).toBeFocused();
      } finally {
        await deleteSpace(api, key);
      }
    });
  }
});
