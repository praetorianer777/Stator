import { crc32 } from "node:zlib";
import type { Page, TestInfo } from "@playwright/test";
import type { StatorApi } from "../fixtures/api";
import { expect } from "../fixtures/auth";
import { orgTest as test } from "../fixtures/org";
import { uniqueName } from "../fixtures/seed";
import { createPage, createSpace, deleteSpace, uniqueKey, type Page as WikiPage } from "../fixtures/spaces";

// The first conversion of a document starts the office suite's work from
// cold, which takes the converter several seconds on a busy machine.
const CONVERSION_MS = 30_000;
const SPEC_MS = 60_000;

// A zip's local header, central entry and end record carry these signatures.
const ZIP_LOCAL = 0x04034b50;
const ZIP_CENTRAL = 0x02014b50;
const ZIP_END = 0x06054b50;
const ZIP_VERSION = 20;

/** A zip that stores its parts uncompressed, which is all a document needs to be one. */
function zipOf(parts: Record<string, string>): Buffer {
  const locals: Buffer[] = [];
  const centrals: Buffer[] = [];
  let offset = 0;
  for (const [name, text] of Object.entries(parts)) {
    const fileName = Buffer.from(name);
    const data = Buffer.from(text);
    const sum = crc32(data);
    const local = Buffer.alloc(30);
    local.writeUInt32LE(ZIP_LOCAL, 0);
    local.writeUInt16LE(ZIP_VERSION, 4);
    local.writeUInt32LE(sum, 14);
    local.writeUInt32LE(data.length, 18);
    local.writeUInt32LE(data.length, 22);
    local.writeUInt16LE(fileName.length, 26);
    const central = Buffer.alloc(46);
    central.writeUInt32LE(ZIP_CENTRAL, 0);
    central.writeUInt16LE(ZIP_VERSION, 4);
    central.writeUInt16LE(ZIP_VERSION, 6);
    central.writeUInt32LE(sum, 16);
    central.writeUInt32LE(data.length, 20);
    central.writeUInt32LE(data.length, 24);
    central.writeUInt16LE(fileName.length, 28);
    central.writeUInt32LE(offset, 42);
    locals.push(local, fileName, data);
    centrals.push(central, fileName);
    offset += local.length + fileName.length + data.length;
  }
  const directory = Buffer.concat(centrals);
  const end = Buffer.alloc(22);
  end.writeUInt32LE(ZIP_END, 0);
  end.writeUInt16LE(Object.keys(parts).length, 8);
  end.writeUInt16LE(Object.keys(parts).length, 10);
  end.writeUInt32LE(directory.length, 12);
  end.writeUInt32LE(offset, 16);
  return Buffer.concat([...locals, directory, end]);
}

/** The smallest word processing document an office suite opens: one paragraph. */
function docx(text: string): Buffer {
  return zipOf({
    "[Content_Types].xml":
      '<?xml version="1.0" encoding="UTF-8"?><Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types"><Default Extension="rels" ContentType="application/vnd.openxmlformats-package.relationships+xml"/><Default Extension="xml" ContentType="application/xml"/><Override PartName="/word/document.xml" ContentType="application/vnd.openxmlformats-officedocument.wordprocessingml.document.main+xml"/></Types>',
    "_rels/.rels":
      '<?xml version="1.0" encoding="UTF-8"?><Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships"><Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/officeDocument" Target="word/document.xml"/></Relationships>',
    "word/document.xml": `<?xml version="1.0" encoding="UTF-8"?><w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main"><w:body><w:p><w:r><w:t>${text}</w:t></w:r></w:p></w:body></w:document>`,
  });
}

const PDF = Buffer.from("%PDF-1.4\n1 0 obj << /Type /Catalog >> endobj\ntrailer << /Root 1 0 R >>\n%%EOF\n");
const DOCX_TYPE = "application/vnd.openxmlformats-officedocument.wordprocessingml.document";

const panel = (page: Page) => page.locator("[data-attachments]");
const row = (page: Page, name: string) => panel(page).locator(`[data-attachment="${name}"]`);

test.describe("attachment previews", { tag: ["@auth"] }, () => {
  const made: string[] = [];
  test.afterEach(async ({ api }) => {
    for (const key of made.splice(0)) await deleteSpace(api, key);
  });

  async function aPage(api: StatorApi, testInfo: TestInfo): Promise<{ key: string; page: WikiPage }> {
    const key = uniqueKey(testInfo);
    made.push(key);
    const space = await createSpace(api, key, uniqueName(testInfo, "Previews"));
    return { key, page: await createPage(api, space.homePageId, "Reports") };
  }

  test("a PDF and an office document are shown in place, and anything else is refused a preview", async ({ page, api }, testInfo) => {
    test.setTimeout(SPEC_MS);
    const { key, page: wiki } = await aPage(api, testInfo);
    await page.goto(`/s/${key}/p/${wiki.id}/reports`);
    const input = panel(page).locator("[data-attachment-input]");
    await input.setInputFiles([
      { name: "report.pdf", mimeType: "application/pdf", buffer: PDF },
      { name: "plan.docx", mimeType: DOCX_TYPE, buffer: docx("Quarterly plan") },
      { name: "notes.txt", mimeType: "text/plain", buffer: Buffer.from("Plain words.") },
    ]);
    for (const name of ["report.pdf", "plan.docx", "notes.txt"]) await expect(row(page, name)).toBeVisible();
    await expect(row(page, "notes.txt").getByRole("button", { name: "Preview notes.txt" })).toHaveCount(0);
    const notes = await row(page, "notes.txt").getAttribute("data-attachment-id");
    const refused = await page.request.get(`/api/v1/attachments/${notes}/preview`);
    expect(refused.status()).toBe(415);
    expect((await refused.json()).error.message).toBe("This kind of file has no preview. Download it to open it.");

    await row(page, "report.pdf").getByRole("button", { name: "Preview report.pdf" }).click();
    let dialog = page.getByRole("dialog", { name: "Preview of report.pdf" });
    await expect(dialog.locator("[data-preview-frame]")).toHaveAttribute("src", /^blob:/);
    await dialog.getByRole("button", { name: "Close" }).click();
    await expect(dialog).toHaveCount(0);

    await row(page, "plan.docx").getByRole("button", { name: "Preview plan.docx" }).click();
    dialog = page.getByRole("dialog", { name: "Preview of plan.docx" });
    await expect(dialog.locator("[data-preview-frame]")).toHaveAttribute("src", /^blob:/, { timeout: CONVERSION_MS });
    await expect(dialog.getByRole("link", { name: "Download plan.docx" })).toBeVisible();
    const newTab = dialog.getByRole("link", { name: "Open in a new tab" });
    const href = await newTab.getAttribute("href");
    expect(href).toMatch(/\/api\/v1\/attachments\/[0-9a-f-]+\/preview$/);

    // What the frame shows is the converted PDF, kept for the next reader.
    const converted = await page.request.get(String(href));
    expect(converted.status()).toBe(200);
    expect(converted.headers()["content-type"]).toBe("application/pdf");
    expect(converted.headers()["content-disposition"]).toBe("inline; filename=plan.pdf");
    expect((await converted.body()).subarray(0, 5).toString()).toBe("%PDF-");
    await page.keyboard.press("Escape");
    await expect(dialog).toHaveCount(0);
  });
});
