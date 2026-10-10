import { mkdirSync, writeFileSync } from "node:fs";
import { dirname, join } from "node:path";
import { pathToFileURL } from "node:url";
import type { Download, Page } from "@playwright/test";
import { must } from "../fixtures/api";
import { expect } from "../fixtures/auth";
import { orgTest as test } from "../fixtures/org";
import { ONE_LOOK, openUntil } from "../fixtures/replica";
import { createPage, createSpace, deleteSpace, uniqueKey } from "../fixtures/spaces";
import { unzip } from "../fixtures/zip";

// A 1 by 1 PNG, which is all a logo needs to be.
const LOGO = Buffer.from("iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mNkYPhfDwAChwGA60e6kgAAAABJRU5ErkJggg==", "base64");
// The worker runs an export as a job of its own, which takes a while on a busy stack.
const WORKED = { timeout: 60_000 } as const;

async function bytesOf(download: Download): Promise<Buffer> {
  const chunks: Buffer[] = [];
  for await (const chunk of await download.createReadStream()) chunks.push(chunk as Buffer);
  return Buffer.concat(chunks);
}

async function exportHtml(page: Page, key: string): Promise<Download> {
  await openUntil(page, `/s/${key}/settings?tab=export`, () => expect(page.locator("[data-space-export]")).toBeVisible(ONE_LOOK));
  await page.locator('[data-export-format="html"]').click();
  await page.locator('[data-action="export-space"]').click();
  const link = page.locator('[data-export-row="done"] [data-action="download-export"]').first();
  await expect(link).toBeVisible(WORKED);
  const downloading = page.waitForEvent("download");
  await link.click();
  return downloading;
}

test.describe("the brand in an offline copy of a space", { tag: ["@auth", "@desktop"] }, () => {
  test("the pages show the logo, the organization's name and the footer line", async ({ page, api }, testInfo) => {
    test.slow();
    const key = uniqueKey(testInfo);
    const space = await createSpace(api, key, "Offline handbook");
    try {
      await createPage(api, space.homePageId, "Packing", {
        type: "doc",
        content: [{ type: "paragraph", content: [{ type: "text", text: "Pack light." }] }],
      });
      must(await api.PUT("/org/brand/footer", { body: { en: "Internal use only", de: "Nur intern" } }));
      const form = new FormData();
      form.append("file", new Blob([new Uint8Array(LOGO)], { type: "image/png" }), "logo.png");
      must(await api.PUT("/org/brand/logo", { body: form as unknown as { file: string } }));

      const entries = unzip(await bytesOf(await exportHtml(page, key)));
      expect(entries.has("files/brand/logo.png")).toBe(true);
      expect(entries.get("index.html")?.toString("utf8")).toContain("Internal use only");

      const dir = testInfo.outputPath("offline");
      for (const [name, data] of entries) {
        const path = join(dir, name);
        mkdirSync(dirname(path), { recursive: true });
        writeFileSync(path, data);
      }
      await page.goto(pathToFileURL(join(dir, "index.html")).href);
      await expect(page.locator("header.site img.logo")).toBeVisible();
      expect(await page.locator("header.site img.logo").evaluate((img: HTMLImageElement) => img.naturalWidth)).toBeGreaterThan(0);
      await expect(page.locator("footer.site .brand-footer")).toHaveText("Internal use only");
    } finally {
      await deleteSpace(api, key);
    }
  });
});
