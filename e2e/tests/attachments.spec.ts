import { mkdir, truncate, writeFile } from "node:fs/promises";
import { dirname } from "node:path";
import type { Locator, Page, TestInfo } from "@playwright/test";
import { expect } from "../fixtures/auth";
import { must, type StatorApi } from "../fixtures/api";
import { orgTest as test } from "../fixtures/org";
import { uniqueName } from "../fixtures/seed";
import { expectAccessible, startInScheme, type ColourScheme } from "../fixtures/shell";
import { createPage, createSpace, deleteSpace, uniqueKey, type Page as WikiPage } from "../fixtures/spaces";

// One pixel, as small as a PNG gets, so a picture costs the suite nothing.
const PIXEL_PNG = Buffer.from("iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mNkYPhfDwAChwGA60e6kgAAAABJRU5ErkJggg==", "base64");
// The compose stack takes the API's default, attachment.DefaultMaxSize.
const UPLOAD_LIMIT_BYTES = 50 * 1024 * 1024;
const OVERSIZE_BYTES = UPLOAD_LIMIT_BYTES + 1024 * 1024;

/** A file past the limit, written sparse to the test's own folder: Playwright takes no buffer that large. */
async function oversizeFile(testInfo: TestInfo, name: string): Promise<string> {
  const path = testInfo.outputPath(name);
  await mkdir(dirname(path), { recursive: true });
  await truncate(path, OVERSIZE_BYTES).catch(async () => {
    await writeFile(path, "");
    await truncate(path, OVERSIZE_BYTES);
  });
  return path;
}

const panel = (page: Page) => page.locator("[data-attachments]");
const row = (page: Page, name: string) => panel(page).locator(`[data-attachment="${name}"]`);
const editorBox = (page: Page) => page.locator("#page-body");

type Payload = { name: string; mimeType: string; buffer: Buffer };
const textFile = (name: string, text: string): Payload => ({ name, mimeType: "text/plain", buffer: Buffer.from(text) });
const picture = (name = "pixel.png"): Payload => ({ name, mimeType: "image/png", buffer: PIXEL_PNG });

/** Hands files to an element the way a drag from the desktop or a paste from the clipboard does. */
async function dispatchFiles(target: Locator, kind: "drop" | "paste", files: Payload[]) {
  const wire = files.map((f) => ({ name: f.name, mimeType: f.mimeType, data: f.buffer.toString("base64") }));
  await target.evaluate(
    (el, { kind, wire }) => {
      const transfer = new DataTransfer();
      for (const f of wire) {
        const bytes = Uint8Array.from(atob(f.data), (c) => c.charCodeAt(0));
        transfer.items.add(new File([bytes], f.name, { type: f.mimeType }));
      }
      if (kind === "paste") {
        el.dispatchEvent(new ClipboardEvent("paste", { clipboardData: transfer, bubbles: true, cancelable: true }));
        return;
      }
      const box = el.getBoundingClientRect();
      const at = {
        clientX: box.left + box.width / 2,
        clientY: box.top + Math.min(box.height / 2, 20),
        bubbles: true,
        cancelable: true,
        dataTransfer: transfer,
      };
      for (const type of ["dragenter", "dragover", "drop"]) el.dispatchEvent(new DragEvent(type, at));
    },
    { kind, wire },
  );
}

async function attachmentsOf(api: StatorApi, pageId: string) {
  const { data } = await api.GET("/pages/{pageID}/attachments", { params: { path: { pageID: pageId } } });
  return data?.attachments ?? [];
}

test.describe("attachments", { tag: ["@auth"] }, () => {
  const made: string[] = [];
  test.afterEach(async ({ api }) => {
    for (const key of made.splice(0)) await deleteSpace(api, key);
  });

  async function aPage(api: StatorApi, testInfo: TestInfo, title = "Runbook"): Promise<{ key: string; page: WikiPage }> {
    const key = uniqueKey(testInfo);
    made.push(key);
    const space = await createSpace(api, key, uniqueName(testInfo, "Files"));
    return { key, page: await createPage(api, space.homePageId, title) };
  }

  test("files are attached by the picker and by dropping, downloaded and deleted", async ({ page, api }, testInfo) => {
    const { key, page: wiki } = await aPage(api, testInfo);
    await page.goto(`/s/${key}/p/${wiki.id}/runbook`);
    await expect(panel(page)).toContainText("Nothing attached yet.");

    const chooser = page.waitForEvent("filechooser");
    await panel(page).locator('[data-action="attach-files"]').click();
    await (await chooser).setFiles([textFile("notes.txt", "Restart the worker first.")]);
    await expect(row(page, "notes.txt")).toBeVisible();
    await expect(page.locator("[data-attachment-notice]")).toHaveText("Attached notes.txt.");

    await dispatchFiles(panel(page), "drop", [picture("dropped.png")]);
    await expect(row(page, "dropped.png")).toBeVisible();
    await expect(panel(page).getByRole("heading")).toHaveText("Attachments (2)");
    // Latest first, as the API lists them.
    await expect(panel(page).locator("[data-attachment]").first()).toHaveAttribute("data-attachment", "dropped.png");
    expect((await attachmentsOf(api, wiki.id)).map((a) => a.fileName)).toEqual(["dropped.png", "notes.txt"]);

    const download = page.waitForEvent("download");
    await row(page, "notes.txt").locator('[data-action="download-attachment"]').click();
    const file = await download;
    expect(file.suggestedFilename()).toBe("notes.txt");
    const saved = await file.createReadStream();
    const chunks: Buffer[] = [];
    for await (const chunk of saved) chunks.push(chunk as Buffer);
    expect(Buffer.concat(chunks).toString()).toBe("Restart the worker first.");

    const asked: string[] = [];
    page.once("dialog", (dialog) => {
      asked.push(dialog.message());
      void dialog.dismiss();
    });
    await row(page, "notes.txt").locator('[data-action="delete-attachment"]').click();
    await expect(row(page, "notes.txt")).toBeVisible();
    page.once("dialog", (dialog) => void dialog.accept());
    await row(page, "notes.txt").locator('[data-action="delete-attachment"]').click();
    await expect(page.locator("[data-attachment-notice]")).toHaveText("Deleted notes.txt.");
    await expect(row(page, "notes.txt")).toHaveCount(0);
    expect(asked[0]).toContain("Delete notes.txt for good?");
    expect((await attachmentsOf(api, wiki.id)).map((a) => a.fileName)).toEqual(["dropped.png"]);
  });

  test("a picture pasted into the editor is uploaded, shown and kept with the page", async ({ page, api }, testInfo) => {
    const { key, page: wiki } = await aPage(api, testInfo);
    await page.goto(`/s/${key}/p/${wiki.id}/runbook/edit`);
    await editorBox(page).click();
    await page.keyboard.type("Before the picture.");
    await dispatchFiles(editorBox(page), "paste", [picture("pasted.png")]);
    const image = editorBox(page).locator("figure[data-image] img");
    await expect(image).toBeVisible();
    await expect.poll(() => image.evaluate((img: HTMLImageElement) => img.naturalWidth)).toBe(1);

    // Alternative text and width, from the image's own tools.
    await image.click();
    await page.getByLabel("Alternative text").fill("A single pixel");
    await page.getByRole("combobox", { name: "Width" }).selectOption({ label: "Small" });

    await dispatchFiles(editorBox(page), "paste", [textFile("steps.txt", "1. Look.")]);
    await expect(editorBox(page).locator("[data-attachment-chip]")).toHaveText("steps.txt");
    await page.locator('[data-action="save-page"]').click();
    await expect(page).toHaveURL(new RegExp(`/p/${wiki.id}/runbook$`));

    const doc = page.locator("[data-doc]");
    const shown = doc.getByRole("img", { name: "A single pixel" });
    await expect(shown).toBeVisible();
    await expect.poll(() => shown.evaluate((img: HTMLImageElement) => img.naturalWidth)).toBe(1);
    await expect(shown).toHaveCSS("width", "240px");
    await expect(doc.getByRole("link", { name: "Download steps.txt" })).toBeVisible();
    await expect(row(page, "pasted.png")).toBeVisible();

    const { page: saved } = must(await api.GET("/pages/{pageID}", { params: { path: { pageID: wiki.id } } }));
    const blocks = (saved.body as { content: Array<{ type: string; attrs?: unknown; content?: Array<{ type: string; attrs?: unknown }> }> }).content;
    const files = await attachmentsOf(api, wiki.id);
    const idOf = (name: string) => files.find((a) => a.fileName === name)?.id;
    expect(blocks.find((b) => b.type === "image")?.attrs).toEqual({ attachmentId: idOf("pasted.png"), alt: "A single pixel", width: 240 });
    const chips = blocks.flatMap((b) => b.content ?? []).filter((n) => n.type === "attachment");
    expect(chips.map((c) => c.attrs)).toEqual([{ attachmentId: idOf("steps.txt"), fileName: "steps.txt" }]);
  });

  test("a deleted file shows as missing where the page used it", async ({ page, api }, testInfo) => {
    const { key, page: wiki } = await aPage(api, testInfo);
    await page.goto(`/s/${key}/p/${wiki.id}/runbook/edit`);
    await editorBox(page).click();
    await dispatchFiles(editorBox(page), "paste", [picture("gone.png")]);
    await expect(editorBox(page).locator("figure[data-image] img")).toBeVisible();
    await page.locator('[data-action="save-page"]').click();
    await expect(page).toHaveURL(new RegExp(`/p/${wiki.id}/runbook$`));

    page.once("dialog", (dialog) => void dialog.accept());
    await row(page, "gone.png").locator('[data-action="delete-attachment"]').click();
    await expect(row(page, "gone.png")).toHaveCount(0);
    await expect(page.locator("[data-doc] [data-image-missing]")).toHaveText("This image was deleted. Remove it from the page, or attach the picture again.");
  });

  test("a file over the limit is refused with a sentence that says what to do", async ({ page, api }, testInfo) => {
    const { key, page: wiki } = await aPage(api, testInfo);
    await page.goto(`/s/${key}/p/${wiki.id}/runbook`);
    await panel(page)
      .locator("[data-attachment-input]")
      .setInputFiles(await oversizeFile(testInfo, "backup.tar"));
    const refusal = panel(page).getByRole("alert");
    await expect(refusal).toContainText("backup.tar (51 MB) is larger than this site accepts.", { timeout: 15_000 });
    await expect(refusal).toContainText("Make the file smaller, or split it into parts, and attach it again.");
    expect(await attachmentsOf(api, wiki.id)).toEqual([]);
  });

  test("a member who may not edit the page sees its files but cannot change them", async ({ page, api, pageAs, apiAs, freshOrg }, testInfo) => {
    const { key, page: wiki } = await aPage(api, testInfo);
    await page.goto(`/s/${key}/p/${wiki.id}/runbook`);
    await panel(page).locator("[data-attachment-input]").setInputFiles(textFile("handover.txt", "Keys are in the drawer."));
    await expect(row(page, "handover.txt")).toBeVisible();

    const bobApi = await apiAs("bob");
    const restricted = await api.PUT("/pages/{pageID}/restrictions", {
      params: { path: { pageID: wiki.id } },
      body: { view: [], edit: [{ type: "user", id: String((await api.GET("/auth/me")).data?.user.id) }] },
    });
    test.skip(restricted.response.status === 501, `Page restrictions (#19) are not built yet, so every member of ${freshOrg.slug} edits every page.`);
    expect(restricted.response.status).toBe(200);

    const bob = await pageAs("bob");
    await bob.goto(`/s/${key}/p/${wiki.id}/runbook`);
    await expect(row(bob, "handover.txt")).toBeVisible();
    await expect(panel(bob).locator('[data-action="attach-files"]')).toHaveCount(0);
    await expect(panel(bob).locator('[data-action="delete-attachment"]')).toHaveCount(0);

    // The API refuses what the page no longer offers.
    const [file] = await attachmentsOf(bobApi, wiki.id);
    const refused = await bobApi.DELETE("/attachments/{attachmentID}", { params: { path: { attachmentID: String(file?.id) } } });
    expect(refused.response.status).toBe(403);
  });

  for (const scheme of ["light", "dark"] as ColourScheme[]) {
    test(`the panel, the pictures and the image tools pass axe in ${scheme}`, async ({ page, api }, testInfo) => {
      await startInScheme(page, scheme);
      const { key, page: wiki } = await aPage(api, testInfo);
      await page.goto(`/s/${key}/p/${wiki.id}/runbook/edit`);
      await editorBox(page).click();
      await dispatchFiles(editorBox(page), "paste", [picture("chart.png")]);
      await dispatchFiles(editorBox(page), "paste", [textFile("data.txt", "1,2,3")]);
      await dispatchFiles(editorBox(page), "paste", [picture("old.png")]);
      await expect(editorBox(page).locator("figure[data-image] img")).toHaveCount(2);
      await expect(editorBox(page).locator("[data-attachment-chip]")).toHaveCount(1);
      await editorBox(page).locator("figure[data-image] img").first().click();
      await page.getByLabel("Alternative text").fill("A chart");
      await expect(page.locator('[data-editor-tools="image"]')).toBeVisible();
      await expectAccessible(page);

      await page.locator('[data-action="save-page"]').click();
      await expect(page).toHaveURL(new RegExp(`/p/${wiki.id}/runbook$`));
      page.once("dialog", (dialog) => void dialog.accept());
      await row(page, "old.png").locator('[data-action="delete-attachment"]').click();
      await expect(page.locator("[data-doc] [data-image-missing]")).toBeVisible();
      await panel(page)
        .locator("[data-attachment-input]")
        .setInputFiles(await oversizeFile(testInfo, "huge.bin"));
      await expect(panel(page).getByRole("alert")).toBeVisible({ timeout: 15_000 });
      await expectAccessible(page);
    });
  }
});
