import { must } from "../fixtures/api";
import { expect } from "../fixtures/auth";
import { orgTest as test } from "../fixtures/org";
import { createPage, createSpace, deleteSpace, uniqueKey } from "../fixtures/spaces";

// A 1 by 1 PNG, which is all a logo needs to be.
const LOGO = Buffer.from("iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mNkYPhfDwAChwGA60e6kgAAAABJRU5ErkJggg==", "base64");
const PRINTED = { timeout: 30_000 } as const;
const IMAGE = /\/Subtype\s*\/Image/g;

test.describe("the brand in a PDF", { tag: ["@auth", "@desktop"] }, () => {
  test("the organization's logo and footer line are on the sheet the PDF is printed from, and the logo is in the file", async ({ page, api }, testInfo) => {
    test.slow();
    const key = uniqueKey(testInfo);
    const space = await createSpace(api, key, "Branded handbook");
    try {
      const guide = await createPage(api, space.homePageId, "Branded guide", {
        type: "doc",
        content: [{ type: "paragraph", content: [{ type: "text", text: "Printed with our brand." }] }],
      });
      const pdf = async () => {
        const { response, data } = await api.GET("/pages/{pageID}/pdf", { params: { path: { pageID: guide.id } }, parseAs: "arrayBuffer" });
        expect(response.status).toBe(200);
        return Buffer.from(data as ArrayBuffer).toString("latin1");
      };
      const before = (await pdf()).match(IMAGE)?.length ?? 0;

      must(await api.PUT("/org/brand/footer", { body: { en: "Internal use only", de: "Nur intern" } }));
      const form = new FormData();
      form.append("file", new Blob([new Uint8Array(LOGO)], { type: "image/png" }), "logo.png");
      must(await api.PUT("/org/brand/logo", { body: form as unknown as { file: string } }));

      await page.goto(`/print/p/${guide.id}`);
      const meta = page.locator("[data-print-meta]");
      await expect(meta).toHaveAttribute("data-brand-footer", "Internal use only", PRINTED);
      await expect(meta).toHaveAttribute("data-logo", /^data:image\/png;base64,/);

      await expect.poll(async () => (await pdf()).match(IMAGE)?.length ?? 0, PRINTED).toBeGreaterThan(before);
    } finally {
      await deleteSpace(api, key);
    }
  });

  test("a page prints without a brand when the organization has none", async ({ page, api }, testInfo) => {
    const key = uniqueKey(testInfo);
    const space = await createSpace(api, key, "Plain handbook");
    try {
      const guide = await createPage(api, space.homePageId, "Plain guide", {
        type: "doc",
        content: [{ type: "paragraph", content: [{ type: "text", text: "No brand here." }] }],
      });
      must(await api.DELETE("/org/brand/logo"));
      must(await api.PUT("/org/brand/footer", { body: { en: "", de: "" } }));
      await page.goto(`/print/p/${guide.id}`);
      await expect(page.locator("html")).toHaveAttribute("data-print-ready", "", PRINTED);
      const meta = page.locator("[data-print-meta]");
      await expect(meta).not.toHaveAttribute("data-logo", /./);
      await expect(meta).not.toHaveAttribute("data-brand-footer", /./);
    } finally {
      await deleteSpace(api, key);
    }
  });
});
