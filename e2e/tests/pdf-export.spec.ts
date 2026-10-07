import type { Download, Page } from "@playwright/test";
import { must } from "../fixtures/api";
import { expect } from "../fixtures/auth";
import { orgTest as test } from "../fixtures/org";
import { ONE_LOOK, openUntil } from "../fixtures/replica";
import { expectAccessible, startInScheme, type ColourScheme } from "../fixtures/shell";
import { createPage, createSpace, deleteSpace, uniqueKey } from "../fixtures/spaces";
import { WEB_URL } from "../fixtures/stack";

const ANONYMOUS = { cookies: [], origins: [] };
/** A print may take as long as the api gives it, STATOR_RENDER_TIMEOUT's 20 seconds, while the suite keeps the stack busy. */
const PRINTED = { timeout: 25_000 } as const;

const heading = (page: Page) => page.locator("main").getByRole("heading", { level: 1 });
const pdfDialog = (page: Page) => page.locator("[data-pdf-export]");
const openTitled = (page: Page, path: string, title: string) => openUntil(page, path, () => expect(heading(page)).toHaveText(title, ONE_LOOK));

async function bytesOf(download: Download): Promise<Buffer> {
  const chunks: Buffer[] = [];
  for await (const chunk of await download.createReadStream()) chunks.push(chunk as Buffer);
  return Buffer.concat(chunks);
}

const TITLE = /\/Title\s*(\((?:\\.|[^\\)])*\)|<[0-9A-Fa-f\s]*>)/;

/** The title a PDF's document information names: Chromium takes it from the printed view's document title. */
function titleOf(pdf: Buffer): string {
  const found = TITLE.exec(pdf.toString("latin1"))?.[1];
  if (!found) throw new Error("the PDF names no title");
  if (found.startsWith("(")) return found.slice(1, -1).replace(/\\([()\\])/g, "$1");
  const bytes = Buffer.from(found.slice(1, -1).replace(/\s/g, ""), "hex");
  if (bytes[0] === 0xfe && bytes[1] === 0xff) return bytes.subarray(2).swap16().toString("utf16le");
  return bytes.toString("latin1");
}

/** Fails unless the bytes are a PDF titled title. */
function expectPdf(bytes: Buffer, title: string) {
  expect(bytes.subarray(0, 5).toString("latin1")).toBe("%PDF-");
  expect(titleOf(bytes)).toBe(title);
}

/**
 * Starts an export and waits for its file. On a stack the whole suite keeps
 * busy a print may run out of the api's time; the dialog then says so and
 * offers Retry, which a reader would press, and this presses it once.
 */
async function exportPdf(page: Page, start: () => Promise<unknown>): Promise<Download> {
  const alert = pdfDialog(page).getByRole("alert");
  const retrying = (await alert.count()) > 0;
  const downloading = page.waitForEvent("download", PRINTED).catch(() => null);
  await start();
  // A retry takes the last failure away before it prints again.
  if (retrying) await expect(alert).toHaveCount(0);
  const file = await Promise.race([
    downloading,
    alert.waitFor(PRINTED).then(
      () => null,
      () => null,
    ),
  ]);
  if (file) return file;
  await expect(alert).toContainText(/took too long to print|Too many PDFs are being made/);
  const again = page.waitForEvent("download", PRINTED);
  await pdfDialog(page).getByRole("button", { name: "Retry" }).click();
  return again;
}

async function openPageMenuItem(page: Page, action: string) {
  await page.locator('[data-action="page-menu"]').click();
  await page.locator(`[data-action="${action}"]`).click();
}

/** A page with a block of most kinds a reader sees drawn: a heading, formulas, a diagram, code and another page included. */
function richBody(word: string, includedId: string) {
  return {
    type: "doc" as const,
    content: [
      { type: "heading", attrs: { level: 1 }, content: [{ type: "text", text: `Setting up ${word}` }] },
      {
        type: "paragraph",
        content: [
          { type: "text", text: "Install it, then " },
          { type: "mathInline", attrs: { latex: "e^{i\\pi} + 1 = 0" } },
        ],
      },
      { type: "diagram", attrs: { source: "flowchart LR\n  install --> configure --> run" } },
      { type: "mathBlock", attrs: { latex: "\\int_0^1 x^2 \\, dx" } },
      { type: "codeBlock", attrs: { language: "go" }, content: [{ type: "text", text: 'fmt.Println("ready")' }] },
      { type: "include", attrs: { pageId: includedId } },
    ],
  };
}

test.describe("PDF export", { tag: ["@auth"] }, () => {
  test("a reader exports a page with diagrams, formulas, code and an include as a PDF, which bob without access cannot", async ({
    page,
    api,
    apiAs,
  }, testInfo) => {
    test.slow();
    const key = uniqueKey(testInfo);
    const word = `pdfword${Date.now().toString(36)}`;
    const space = await createSpace(api, key, "Printed handbook");
    try {
      const included = await createPage(api, space.homePageId, `Included ${word}`, {
        type: "doc",
        content: [{ type: "paragraph", content: [{ type: "text", text: "Words from another page." }] }],
      });
      const guide = await createPage(api, space.homePageId, `Guide ${word}`, richBody(word, included.id));
      await openTitled(page, `/s/${key}/p/${guide.id}/guide`, guide.title);

      const file = await exportPdf(page, () => openPageMenuItem(page, "export-pdf"));
      await expect(pdfDialog(page)).toHaveCount(0);
      expect(file.suggestedFilename()).toMatch(new RegExp(`^${key}-guide-${word}-\\d{4}-\\d{2}-\\d{2}\\.pdf$`));
      expectPdf(await bytesOf(file), guide.title);

      const me = must(await api.GET("/auth/me"));
      must(
        await api.PUT("/pages/{pageID}/restrictions", { params: { path: { pageID: guide.id } }, body: { view: [{ type: "user", id: me.user.id }], edit: [] } }),
      );
      const bob = await apiAs("bob");
      await expect
        .poll(async () => (await bob.GET("/pages/{pageID}/pdf", { params: { path: { pageID: guide.id } }, parseAs: "stream" })).response.status)
        .toBe(404);
    } finally {
      await deleteSpace(api, key);
    }
  });

  test("anybody exports a page anybody may read, from the public view and a public link", async ({ api, browser }, testInfo) => {
    test.slow();
    const key = uniqueKey(testInfo);
    const word = `openpdf${Date.now().toString(36)}`;
    const space = await createSpace(api, key, "Open handbook");
    const context = await browser.newContext({ baseURL: WEB_URL, storageState: ANONYMOUS });
    try {
      const guide = await createPage(api, space.homePageId, `Open ${word}`, {
        type: "doc",
        content: [{ type: "paragraph", content: [{ type: "text", text: "Anybody may read this." }] }],
      });
      must(await api.PUT("/spaces/{spaceKey}/anonymous-access", { params: { path: { spaceKey: key } }, body: { view: true } }));
      must(await api.PUT("/org/anonymous-access", { body: { enabled: true, indexable: false } }));
      const org = must(await api.GET("/auth/me")).organization!.slug;
      const reader = await context.newPage();

      await openTitled(reader, `/public/${org}/s/${key}/p/${guide.id}/open`, guide.title);
      const file = await exportPdf(reader, () => reader.locator('[data-action="export-pdf"]').click());
      expect(file.suggestedFilename()).toMatch(new RegExp(`^${key}-open-${word}-`));
      expectPdf(await bytesOf(file), guide.title);

      const link = must(await api.POST("/pages/{pageID}/public-links", { params: { path: { pageID: guide.id } }, body: {} }));
      await openTitled(reader, link.path, guide.title);
      const linkedFile = await exportPdf(reader, () => reader.locator('[data-action="export-pdf"]').click());
      expect(linkedFile.suggestedFilename()).toMatch(new RegExp(`^open-${word}-`));
      expectPdf(await bytesOf(linkedFile), guide.title);
    } finally {
      await context.close();
      must(await api.PUT("/org/anonymous-access", { body: { enabled: false, indexable: false } }));
      await deleteSpace(api, key);
    }
  });

  for (const scheme of ["light", "dark"] as ColourScheme[]) {
    test(`the PDF dialog passes axe in ${scheme}, while it prints and when the print fails`, async ({ page, api }, testInfo) => {
      test.slow();
      await startInScheme(page, scheme);
      const key = uniqueKey(testInfo);
      const space = await createSpace(api, key, "Accessible prints");
      try {
        const guide = await createPage(api, space.homePageId, `Guide ${key}`);
        await openTitled(page, `/s/${key}/p/${guide.id}/guide`, guide.title);

        // The print is held until axe has looked at the dialog that waits for it.
        let release!: () => void;
        const held = new Promise<void>((resolve) => {
          release = resolve;
        });
        await page.route("**/api/v1/pages/*/pdf", async (route) => {
          await held;
          await route.continue();
        });
        await openPageMenuItem(page, "export-pdf");
        await expect(pdfDialog(page).getByRole("status")).toBeVisible();
        await expectAccessible(page);
        await exportPdf(page, async () => release());
        await expect(pdfDialog(page)).toHaveCount(0);
        await page.unroute("**/api/v1/pages/*/pdf");

        // A connection lost on the way is a failure the reader can retry.
        await page.route("**/api/v1/pages/*/pdf", (route) => route.abort("connectionreset"));
        await openPageMenuItem(page, "export-pdf");
        await expect(pdfDialog(page).getByRole("alert")).toContainText("Check your connection and try again.");
        await expectAccessible(page);
        await page.unroute("**/api/v1/pages/*/pdf");
        await exportPdf(page, () => pdfDialog(page).getByRole("button", { name: "Retry" }).click());
        await expect(pdfDialog(page)).toHaveCount(0);
        await expect(page.locator('[data-action="page-menu"]')).toBeFocused();
      } finally {
        await deleteSpace(api, key);
      }
    });
  }
});
