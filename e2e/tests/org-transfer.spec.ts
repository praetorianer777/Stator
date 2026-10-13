import { writeFileSync } from "node:fs";
import type { Download } from "@playwright/test";
import { createStatorApi, must } from "../fixtures/api";
import { authTest as test, expect } from "../fixtures/auth";
import { withAnotherOrg } from "../fixtures/org";
import { ONE_LOOK, openUntil } from "../fixtures/replica";
import { expectAccessible, startInScheme, type ColourScheme } from "../fixtures/shell";
import { WEB_URL } from "../fixtures/stack";
import { createPage, createSpace, deleteSpace, uniqueKey } from "../fixtures/spaces";
import { unzip } from "../fixtures/zip";

// The worker runs an export or import as a job of its own, which on a stack
// the whole suite keeps busy takes a while to be begun and done.
const WORKED = { timeout: 90_000 } as const;

async function bytesOf(download: Download): Promise<Buffer> {
  const chunks: Buffer[] = [];
  for await (const chunk of await download.createReadStream()) chunks.push(chunk as Buffer);
  return Buffer.concat(chunks);
}

const words = (text: string) => ({ type: "doc" as const, content: [{ type: "paragraph", content: [{ type: "text", text }] }] });

interface OrgManifest {
  format: string;
  spaces: Array<{ key: string; archive: string }>;
  notCarried: Array<{ key: string }>;
}

// As the realm's alice and bob rather than people of the spec's own: the
// archive goes into a second organization alice signs in to through the
// provider, where she must find the draft she wrote here.
test.describe("organization export and import", { tag: ["@auth"] }, () => {
  test("an administrator exports the organization, a member cannot, and a fresh organization takes the archive whole", async ({
    page,
    api,
    browser,
    pageAs,
  }, testInfo) => {
    test.slow();
    const key = uniqueKey(testInfo);
    const space = await createSpace(api, key, "Moving handbook");
    try {
      const packing = await createPage(api, space.homePageId, "Packing", words("Pack light."));
      must(
        await api.PUT("/pages/{pageID}/draft", {
          params: { path: { pageID: packing.id } },
          body: { title: "Packing", body: words("Pack lighter."), baseVersion: 1 },
        }),
      );

      await openUntil(page, "/settings/transfer", () => expect(page.locator("[data-org-transfer]")).toBeVisible(ONE_LOOK));
      await expect(page.locator('[data-not-carried="passwords"]')).toBeVisible();
      // The organization keeps the exports of earlier runs; this one is the one not among them.
      const earlier = new Set(must(await api.GET("/site/exports")).exports.map((each) => each.id));
      await page.locator('[data-action="export-org"]').click();
      const row = page.locator("[data-org-export-row]").first();
      await expect(async () => {
        expect(earlier.has((await row.getAttribute("data-export-id")) ?? "")).toBe(false);
      }).toPass(WORKED);
      await expect(row).toHaveAttribute("data-org-export-row", "done", WORKED);
      const link = row.locator('[data-action="download-org-export"]');
      const downloading = page.waitForEvent("download");
      await link.click();
      const file = await downloading;
      expect(file.suggestedFilename()).toMatch(/-organization-\d{4}-\d{2}-\d{2}\.zip$/);
      const archive = await bytesOf(file);
      const entries = unzip(archive);
      const manifest = JSON.parse(entries.get("manifest.json")!.toString("utf8")) as OrgManifest;
      expect(manifest.format).toBe("stator.organization");
      expect(manifest.notCarried.map((o) => o.key)).toContain("passwords");
      const entry = manifest.spaces.find((s) => s.key === key);
      expect(entry).toBeDefined();
      const inner = unzip(entries.get(entry!.archive)!);
      const spaceManifest = JSON.parse(inner.get("manifest.json")!.toString("utf8")) as { pages: string[] };
      expect(spaceManifest.pages).toContain(packing.id);
      await expectAccessible(page);

      const bob = await pageAs("bob");
      await bob.goto("/settings/transfer");
      await expect(bob.getByText("Only an administrator of the organization exports or imports all of it", { exact: false })).toBeVisible();
      await expect(bob.locator('[data-action="export-org"]')).toHaveCount(0);

      const saved = testInfo.outputPath("organization.zip");
      writeFileSync(saved, archive);
      await withAnotherOrg(browser, "org-transfer-to", async (fresh) => {
        const context = await browser.newContext({ baseURL: WEB_URL, storageState: fresh.sessions.alice });
        try {
          const there = await context.newPage();
          await openUntil(there, "/settings/transfer", () => expect(there.locator("[data-org-import]")).toBeVisible(ONE_LOOK));
          await there.locator("[data-org-import-file]").setInputFiles(saved);
          await there.locator('[data-action="confirm-import-org"]').click();
          await expect(there.locator("[data-org-import-done]")).toBeVisible(WORKED);
          await expect(there.locator("[data-org-import-report]")).toBeVisible();
          await expectAccessible(there);
          await openUntil(there, `/s/${key}`, () => expect(there.getByRole("heading", { level: 1, name: "Moving handbook" })).toBeVisible(ONE_LOOK));
          const thereApi = await createStatorApi(fresh.sessions.alice);
          try {
            const { pages } = must(await thereApi.GET("/spaces/{spaceKey}/outline", { params: { path: { spaceKey: key } } }));
            const copy = pages.find((p) => p.title === "Packing");
            expect(copy).toBeDefined();
            const draft = must(await thereApi.GET("/pages/{pageID}/draft", { params: { path: { pageID: copy!.id } } }));
            expect(JSON.stringify(draft)).toContain("Pack lighter.");
          } finally {
            await thereApi.dispose();
          }

          await there.goto("/settings/transfer");
          await there.locator("[data-org-import-file]").setInputFiles(saved);
          await there.locator('[data-action="confirm-import-org"]').click();
          await expect(there.getByText("This organization has spaces already", { exact: false })).toBeVisible();
        } finally {
          await context.close();
        }
      });
    } finally {
      await deleteSpace(api, key);
    }
  });

  for (const scheme of ["light", "dark"] as ColourScheme[]) {
    test(`the export and import settings are accessible in the ${scheme} scheme`, async ({ page }) => {
      await startInScheme(page, scheme);
      await openUntil(page, "/settings/transfer", () => expect(page.locator("[data-org-transfer]")).toBeVisible(ONE_LOOK));
      await expect(page.locator('[data-action="choose-org-archive"]')).toBeVisible();
      await expectAccessible(page);
    });
  }
});
