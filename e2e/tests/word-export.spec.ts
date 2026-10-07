import { inflateRawSync } from "node:zlib";
import type { Download, Page } from "@playwright/test";
import { must } from "../fixtures/api";
import { expect } from "../fixtures/auth";
import { png, upload } from "../fixtures/files";
import { orgTest as test } from "../fixtures/org";
import { ONE_LOOK, openUntil } from "../fixtures/replica";
import { expectAccessible, startInScheme, type ColourScheme } from "../fixtures/shell";
import { createPage, createSpace, deleteSpace, uniqueKey } from "../fixtures/spaces";
import { WEB_URL } from "../fixtures/stack";

const ANONYMOUS = { cookies: [], origins: [] };
/** An export reads every picture of its page from storage, while the suite keeps the stack busy. */
const EXPORTED = { timeout: 20_000 } as const;

const heading = (page: Page) => page.locator("main").getByRole("heading", { level: 1 });
const wordDialog = (page: Page) => page.locator("[data-docx-export]");
const openTitled = (page: Page, path: string, title: string) => openUntil(page, path, () => expect(heading(page)).toHaveText(title, ONE_LOOK));

async function bytesOf(download: Download): Promise<Buffer> {
  const chunks: Buffer[] = [];
  for await (const chunk of await download.createReadStream()) chunks.push(chunk as Buffer);
  return Buffer.concat(chunks);
}

// The signatures of a zip's end record and of an entry in its directory.
const END_OF_DIRECTORY = 0x06054b50;
const DIRECTORY_ENTRY = 0x02014b50;
const STORED = 0;
const DEFLATED = 8;

/** A zip's files by name, read from its directory, since Word documents are zips and the suite carries no zip library. */
function unzip(zip: Buffer): Map<string, Buffer> {
  let end = zip.length - 22;
  while (end >= 0 && zip.readUInt32LE(end) !== END_OF_DIRECTORY) end--;
  if (end < 0) throw new Error("not a zip: no end of its directory");
  const count = zip.readUInt16LE(end + 10);
  let at = zip.readUInt32LE(end + 16);
  const files = new Map<string, Buffer>();
  for (let i = 0; i < count; i++) {
    if (zip.readUInt32LE(at) !== DIRECTORY_ENTRY) throw new Error("not a zip: a broken directory");
    const method = zip.readUInt16LE(at + 10);
    const size = zip.readUInt32LE(at + 20);
    const nameLength = zip.readUInt16LE(at + 28);
    const extraLength = zip.readUInt16LE(at + 30);
    const commentLength = zip.readUInt16LE(at + 32);
    const local = zip.readUInt32LE(at + 42);
    const name = zip.subarray(at + 46, at + 46 + nameLength).toString("utf8");
    const start = local + 30 + zip.readUInt16LE(local + 26) + zip.readUInt16LE(local + 28);
    const data = zip.subarray(start, start + size);
    if (method !== STORED && method !== DEFLATED) throw new Error(`${name} is packed in a way this reader does not know`);
    files.set(name, method === DEFLATED ? inflateRawSync(data) : data);
    at += 46 + nameLength + extraLength + commentLength;
  }
  return files;
}

/** Fails unless the bytes are a Word document, and answers its parts as text. */
function expectWord(bytes: Buffer): Map<string, string> {
  expect(bytes.subarray(0, 2).toString("latin1")).toBe("PK");
  const parts = new Map([...unzip(bytes)].map(([name, data]) => [name, name.endsWith(".png") ? `${data.length} bytes` : data.toString("utf8")]));
  expect(parts.get("[Content_Types].xml")).toContain("wordprocessingml.document.main+xml");
  expect(parts.has("word/document.xml")).toBe(true);
  return parts;
}

/** Starts an export and waits for its file. */
async function exportWord(page: Page, start: () => Promise<unknown>): Promise<Download> {
  const downloading = page.waitForEvent("download", EXPORTED);
  await start();
  return downloading;
}

async function openPageMenuItem(page: Page, action: string) {
  await page.locator('[data-action="page-menu"]').click();
  await page.locator(`[data-action="${action}"]`).click();
}

const text = (words: string) => ({ type: "text", text: words });
const paragraph = (words: string) => ({ type: "paragraph", content: [text(words)] });
const cell = (type: string, words: string) => ({ type, content: [paragraph(words)] });

/** A page with what a Word document has to keep: a heading, a nested list, a table, a picture and code. */
function wordBody(word: string, pictureId: string) {
  return {
    type: "doc" as const,
    content: [
      { type: "heading", attrs: { level: 1 }, content: [text(`Setting up ${word}`)] },
      {
        type: "bulletList",
        content: [
          {
            type: "listItem",
            content: [paragraph("Outer step"), { type: "orderedList", content: [{ type: "listItem", content: [paragraph("Inner step")] }] }],
          },
        ],
      },
      {
        type: "table",
        content: [
          { type: "tableRow", content: [cell("tableHeader", "Setting"), cell("tableHeader", "Value")] },
          { type: "tableRow", content: [cell("tableCell", "Port"), cell("tableCell", "8080")] },
        ],
      },
      { type: "image", attrs: { attachmentId: pictureId, alt: "The board" } },
      { type: "codeBlock", attrs: { language: "go" }, content: [text('fmt.Println("ready")')] },
    ],
  };
}

test.describe("Word export", { tag: ["@auth"] }, () => {
  test("a reader exports a page with a heading, a nested list, a table, a picture and code as Word, which bob without access cannot", async ({
    page,
    api,
    apiAs,
  }, testInfo) => {
    test.slow();
    const key = uniqueKey(testInfo);
    const word = `docxword${Date.now().toString(36)}`;
    const space = await createSpace(api, key, "Word handbook");
    try {
      const guide = await createPage(api, space.homePageId, `Guide ${word}`);
      const picture = await upload(api, guide.id, "board.png", "image/png", png(40, 20, [30, 120, 200]));
      must(await api.PATCH("/pages/{pageID}", { params: { path: { pageID: guide.id } }, body: { body: wordBody(word, picture), version: 1 } }));
      await openTitled(page, `/s/${key}/p/${guide.id}/guide`, guide.title);

      const file = await exportWord(page, () => openPageMenuItem(page, "export-docx"));
      await expect(wordDialog(page)).toHaveCount(0);
      expect(file.suggestedFilename()).toMatch(new RegExp(`^${key}-guide-${word}-\\d{4}-\\d{2}-\\d{2}\\.docx$`));
      const parts = expectWord(await bytesOf(file));
      const body = parts.get("word/document.xml")!;
      expect(body).toContain('<w:pStyle w:val="Heading1"/>');
      expect(body).toContain(`Setting up ${word}`);
      expect(body).toContain('<w:ilvl w:val="1"/>');
      expect(body).toContain("Inner step");
      expect(body).toContain("<w:tblHeader/>");
      expect(body).toContain("8080");
      expect(body).toContain('descr="The board"');
      expect(parts.get("word/media/picture1.png")).toMatch(/^\d+ bytes$/);
      expect(body).toContain('<w:pStyle w:val="Code"/>');
      expect(body).toContain("fmt.Println(&#34;ready&#34;)");
      expect(parts.get("docProps/core.xml")).toContain(`<dc:title>Guide ${word}</dc:title>`);

      const me = must(await api.GET("/auth/me"));
      must(
        await api.PUT("/pages/{pageID}/restrictions", { params: { path: { pageID: guide.id } }, body: { view: [{ type: "user", id: me.user.id }], edit: [] } }),
      );
      const bob = await apiAs("bob");
      await expect
        .poll(async () => (await bob.GET("/pages/{pageID}/docx", { params: { path: { pageID: guide.id } }, parseAs: "stream" })).response.status)
        .toBe(404);
    } finally {
      await deleteSpace(api, key);
    }
  });

  test("anybody exports a page anybody may read as Word, from the public view and a public link", async ({ api, browser }, testInfo) => {
    test.slow();
    const key = uniqueKey(testInfo);
    const word = `opendocx${Date.now().toString(36)}`;
    const space = await createSpace(api, key, "Open handbook");
    const context = await browser.newContext({ baseURL: WEB_URL, storageState: ANONYMOUS });
    try {
      const guide = await createPage(api, space.homePageId, `Open ${word}`, { type: "doc", content: [paragraph("Anybody may read this.")] });
      must(await api.PUT("/spaces/{spaceKey}/anonymous-access", { params: { path: { spaceKey: key } }, body: { view: true } }));
      must(await api.PUT("/org/anonymous-access", { body: { enabled: true, indexable: false } }));
      const org = must(await api.GET("/auth/me")).organization!.slug;
      const reader = await context.newPage();

      await openTitled(reader, `/public/${org}/s/${key}/p/${guide.id}/open`, guide.title);
      await expect(reader.locator('[data-action="export-pdf"]')).toBeVisible();
      const file = await exportWord(reader, () => reader.locator('[data-action="export-docx"]').click());
      expect(file.suggestedFilename()).toMatch(new RegExp(`^${key}-open-${word}-.*\\.docx$`));
      expect(expectWord(await bytesOf(file)).get("word/document.xml")).toContain("Anybody may read this.");

      const link = must(await api.POST("/pages/{pageID}/public-links", { params: { path: { pageID: guide.id } }, body: {} }));
      await openTitled(reader, link.path, guide.title);
      const linkedFile = await exportWord(reader, () => reader.locator('[data-action="export-docx"]').click());
      expect(linkedFile.suggestedFilename()).toMatch(new RegExp(`^open-${word}-.*\\.docx$`));
      const linked = expectWord(await bytesOf(linkedFile));
      expect(linked.get("word/document.xml")).toContain("Anybody may read this.");
      const token = link.path.split("/").pop()!;
      for (const [, part] of linked) expect(part).not.toContain(token);
      await expectAccessible(reader);
    } finally {
      await context.close();
      must(await api.PUT("/org/anonymous-access", { body: { enabled: false, indexable: false } }));
      await deleteSpace(api, key);
    }
  });

  for (const scheme of ["light", "dark"] as ColourScheme[]) {
    test(`the Word dialog passes axe in ${scheme}, while it exports and when the export fails`, async ({ page, api }, testInfo) => {
      test.slow();
      await startInScheme(page, scheme);
      const key = uniqueKey(testInfo);
      const space = await createSpace(api, key, "Accessible exports");
      try {
        const guide = await createPage(api, space.homePageId, `Guide ${key}`);
        await openTitled(page, `/s/${key}/p/${guide.id}/guide`, guide.title);

        // The export is held until axe has looked at the dialog that waits for it.
        let release!: () => void;
        const held = new Promise<void>((resolve) => {
          release = resolve;
        });
        await page.route("**/api/v1/pages/*/docx", async (route) => {
          await held;
          await route.continue();
        });
        await openPageMenuItem(page, "export-docx");
        await expect(wordDialog(page).getByRole("status")).toBeVisible();
        await expectAccessible(page);
        await exportWord(page, async () => release());
        await expect(wordDialog(page)).toHaveCount(0);
        await page.unroute("**/api/v1/pages/*/docx");

        // A connection lost on the way is a failure the reader can retry.
        await page.route("**/api/v1/pages/*/docx", (route) => route.abort("connectionreset"));
        await openPageMenuItem(page, "export-docx");
        await expect(wordDialog(page).getByRole("alert")).toContainText("Check your connection and try again.");
        await expectAccessible(page);
        await page.unroute("**/api/v1/pages/*/docx");
        await exportWord(page, () => wordDialog(page).getByRole("button", { name: "Retry" }).click());
        await expect(wordDialog(page)).toHaveCount(0);
        await expect(page.locator('[data-action="page-menu"]')).toBeFocused();
      } finally {
        await deleteSpace(api, key);
      }
    });
  }
});
