import type { Page, TestInfo } from "@playwright/test";
import { must, type StatorApi } from "../fixtures/api";
import { expect } from "../fixtures/auth";
import { caretTo } from "../fixtures/editor";
import { orgTest as test } from "../fixtures/org";
import { openShowing } from "../fixtures/replica";
import { uniqueName } from "../fixtures/seed";
import { expectAccessible, startInScheme, type ColourScheme } from "../fixtures/shell";
import { createPage, createSpace, deleteSpace, publishFromEditor, uniqueKey, type Space } from "../fixtures/spaces";

const editorBox = (page: Page) => page.locator("#page-body");
const shown = (page: Page) => page.locator("main [data-doc]");
const paragraph = (value: string) => ({ type: "paragraph", content: [{ type: "text", text: value }] });
const csv = (body: string) => ({ name: "budget.csv", mimeType: "text/csv", buffer: Buffer.from(body) });

test.describe("files block", { tag: ["@auth"] }, () => {
  const made: string[] = [];
  test.afterEach(async ({ api }) => {
    for (const key of made.splice(0)) await deleteSpace(api, key);
  });

  async function freshSpace(api: StatorApi, testInfo: TestInfo, title: string): Promise<Space> {
    const key = uniqueKey(testInfo);
    made.push(key);
    return createSpace(api, key, uniqueName(testInfo, title));
  }

  test("an author lists a page's files in its content, uploads from the block, and a second upload is the next version", async ({ page, api }, testInfo) => {
    const space = await freshSpace(api, testInfo, "Finance");
    const plan = await createPage(api, space.homePageId, "Budget", { type: "doc", content: [paragraph("This year's numbers."), { type: "paragraph" }] });
    const path = `/s/${space.key}/p/${plan.id}/budget`;

    await openShowing(page, `${path}/edit`, "This year's numbers.");
    await caretTo(editorBox(page), "end");
    await page.keyboard.type("/files");
    await expect(page.getByRole("listbox", { name: "Insert a block" })).toBeVisible();
    await page.keyboard.press("Enter");
    const block = editorBox(page).getByRole("region", { name: "Files on this page" });
    await expect(block).toContainText("No files on this page yet.");
    await block.locator("[data-attachment-list-input]").setInputFiles(csv("q1,10\n"));
    await expect(block.locator("[data-listed-file]")).toHaveAttribute("data-version", "1");
    await block.locator("[data-attachment-list-input]").setInputFiles(csv("q1,10\nq2,12\n"));
    await expect(block.getByRole("status")).toHaveText("Uploaded budget.csv as version 2.");
    await expect(block.locator("[data-listed-file]")).toHaveCount(1);
    await expect(page.locator("[data-draft-status]")).toHaveAttribute("data-draft-status", "saved");
    await publishFromEditor(page);

    const list = shown(page).getByRole("region", { name: "Files on this page" });
    const budget = list.locator('[data-listed-file="budget.csv"]');
    await expect(budget).toContainText(/Version 2 · .+, /);
    await budget.getByText("1 earlier version").click();
    const earlier = budget.getByRole("link", { name: "Download budget.csv, version 1" });
    await expect(earlier).toBeVisible();
    const href = await earlier.getAttribute("href");
    const old = await page.request.get(href!);
    expect(await old.text()).toBe("q1,10\n");
    // The panel below the page lists the name once, with its earlier version under it.
    await expect(page.locator("[data-attachments] [data-attachment]")).toHaveCount(1);
    await expect(page.locator('[data-attachments] [data-attachment="budget.csv"]')).toContainText("1 earlier version");
  });

  for (const scheme of ["light", "dark"] as ColourScheme[]) {
    test(`the files block passes axe in ${scheme}`, async ({ page, api }, testInfo) => {
      const space = await freshSpace(api, testInfo, "Axe");
      const target = await createPage(api, space.homePageId, "Files", { type: "doc", content: [paragraph("Files."), { type: "attachmentList" }] });
      for (const content of ["one", "two"]) {
        const form = new FormData();
        form.append("file", new Blob([content], { type: "text/plain" }), "notes.txt");
        // The generated type describes the multipart fields as strings; the form itself is what is sent.
        const body = form as unknown as { file: string };
        must(await api.POST("/pages/{pageID}/attachments", { params: { path: { pageID: target.id } }, body }));
      }
      await startInScheme(page, scheme);
      await openShowing(page, `/s/${space.key}/p/${target.id}/files`, "notes.txt");
      await shown(page).getByText("1 earlier version").click();
      await page.screenshot({ path: testInfo.outputPath(`files-${scheme}.png`), fullPage: true });
      await expectAccessible(page);
    });
  }
});
