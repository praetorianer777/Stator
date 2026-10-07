import { mkdirSync, writeFileSync } from "node:fs";
import { dirname, join } from "node:path";
import { pathToFileURL } from "node:url";
import type { Download, Page } from "@playwright/test";
import { must, type StatorApi } from "../fixtures/api";
import { expect } from "../fixtures/auth";
import { orgTest as test } from "../fixtures/org";
import { ONE_LOOK, openUntil } from "../fixtures/replica";
import { expectAccessible, startInScheme, type ColourScheme } from "../fixtures/shell";
import { createPage, createSpace, deleteSpace, uniqueKey } from "../fixtures/spaces";
import { unzip } from "../fixtures/zip";

// The worker runs an export or import as a job of its own, which on a stack
// the whole suite keeps busy takes a while to be begun and done.
const WORKED = { timeout: 60_000 } as const;

async function bytesOf(download: Download): Promise<Buffer> {
  const chunks: Buffer[] = [];
  for await (const chunk of await download.createReadStream()) chunks.push(chunk as Buffer);
  return Buffer.concat(chunks);
}

const words = (text: string) => ({ type: "doc" as const, content: [{ type: "paragraph", content: [{ type: "text", text }] }] });

/** Publishes the page again with other words, as its second version. */
async function publishAgain(api: StatorApi, pageId: string, title: string, text: string) {
  must(await api.PUT("/pages/{pageID}/draft", { params: { path: { pageID: pageId } }, body: { title, body: words(text), baseVersion: 1 } }));
  must(await api.POST("/pages/{pageID}/publish", { params: { path: { pageID: pageId } }, body: { notifyWatchers: false, comment: "Second thoughts" } }));
}

/** Exports the space from its settings in the format given and downloads the file. */
async function exportFromSettings(page: Page, key: string, format: "archive" | "html"): Promise<Download> {
  await openUntil(page, `/s/${key}/settings?tab=export`, () => expect(page.locator("[data-space-export]")).toBeVisible(ONE_LOOK));
  await page.locator(`[data-export-format="${format}"]`).click();
  await page.locator('[data-action="export-space"]').click();
  const link = page.locator('[data-export-row="done"] [data-action="download-export"]').first();
  await expect(link).toBeVisible(WORKED);
  const downloading = page.waitForEvent("download");
  await link.click();
  return downloading;
}

test.describe("space export and import", { tag: ["@auth"] }, () => {
  test("an administrator exports a space, downloads it and imports it under a new key with its history", async ({ page, api }, testInfo) => {
    test.slow();
    const key = uniqueKey(testInfo);
    const landed = `L${key.slice(1)}`;
    const space = await createSpace(api, key, "Travel handbook");
    try {
      const packing = await createPage(api, space.homePageId, "Packing", words("Pack light."));
      await publishAgain(api, packing.id, "Packing", "Pack light, and a towel.");

      const file = await exportFromSettings(page, key, "archive");
      expect(file.suggestedFilename()).toBe(`${key.toLowerCase()}-space.zip`);
      const archive = await bytesOf(file);
      const entries = unzip(archive);
      const manifest = JSON.parse(entries.get("manifest.json")!.toString("utf8")) as { format: string; space: { key: string }; pages: string[] };
      expect(manifest.format).toBe("stator.space");
      expect(manifest.space.key).toBe(key);
      expect(manifest.pages).toContain(packing.id);
      await expectAccessible(page);

      const saved = testInfo.outputPath("space.zip");
      writeFileSync(saved, archive);
      await page.goto("/spaces");
      await page.locator('[data-action="import-space"]').click();
      await page.locator("[data-import-file]").setInputFiles(saved);
      await page.getByLabel("Key", { exact: true }).fill(landed);
      await page.locator('[data-action="confirm-import-space"]').click();
      await expect(page.locator("[data-import-done]")).toBeVisible(WORKED);
      await expect(page.locator("[data-import-report]")).toContainText(/2 pages with \d+ versions/);
      await expectAccessible(page);
      await page.locator('[data-action="open-imported"]').click();
      await expect(page).toHaveURL(new RegExp(`/s/${landed}`));

      const { pages } = must(await api.GET("/spaces/{spaceKey}/outline", { params: { path: { spaceKey: landed } } }));
      const copy = pages.find((p) => p.title === "Packing");
      expect(copy).toBeDefined();
      await openUntil(page, `/s/${landed}/p/${copy!.id}/packing/history`, () => expect(page.getByText("Second thoughts")).toBeVisible(ONE_LOOK));
      await expect(page.locator("[data-version-row]")).toHaveCount(2);
    } finally {
      await deleteSpace(api, key);
      await deleteSpace(api, landed);
    }
  });

  test("the HTML export opens offline, its pages linked to each other", async ({ page, api }, testInfo) => {
    test.slow();
    const key = uniqueKey(testInfo);
    const space = await createSpace(api, key, "Offline handbook");
    try {
      const packing = await createPage(api, space.homePageId, "Packing", words("Pack light, and a towel."));
      const archive = await bytesOf(await exportFromSettings(page, key, "html"));
      const entries = unzip(archive);
      const index = entries.get("index.html")?.toString("utf8") ?? "";
      expect(index).toContain('href="packing.html"');
      expect(entries.get("packing.html")?.toString("utf8")).toContain("Pack light, and a towel.");
      for (const [name, data] of entries) expect(data.toString("utf8"), name).not.toContain("<script");

      const dir = testInfo.outputPath("offline");
      for (const [name, data] of entries) {
        const path = join(dir, name);
        mkdirSync(dirname(path), { recursive: true });
        writeFileSync(path, data);
      }
      await page.goto(pathToFileURL(join(dir, "index.html")).href);
      await expect(page.getByRole("heading", { level: 1, name: "Offline handbook" })).toBeVisible();
      await page.getByRole("link", { name: packing.title }).first().click();
      await expect(page.getByRole("heading", { level: 1, name: "Packing" })).toBeVisible();
      await expect(page.locator("article")).toContainText("Pack light, and a towel.");
    } finally {
      await deleteSpace(api, key);
    }
  });

  for (const scheme of ["light", "dark"] as ColourScheme[]) {
    test(`the export settings and the import page are accessible in the ${scheme} scheme`, async ({ page, api }, testInfo) => {
      const key = uniqueKey(testInfo);
      await createSpace(api, key, "Accessible handbook");
      try {
        await startInScheme(page, scheme);
        await openUntil(page, `/s/${key}/settings?tab=export`, () => expect(page.locator("[data-space-export]")).toBeVisible(ONE_LOOK));
        await expect(page.getByText("This space has not been exported yet.")).toBeVisible();
        await expectAccessible(page);
        await page.goto("/spaces/import");
        await expect(page.locator('[data-action="choose-archive"]')).toBeVisible();
        await expectAccessible(page);
      } finally {
        await deleteSpace(api, key);
      }
    });
  }
});
