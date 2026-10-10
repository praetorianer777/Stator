import { must } from "../fixtures/api";
import { expect } from "../fixtures/auth";
import { orgTest as test } from "../fixtures/org";
import { createPage, createSpace, deleteSpace, uniqueKey } from "../fixtures/spaces";
import { unzip } from "../fixtures/zip";

// A 1 by 1 PNG, which is all a logo needs to be.
const LOGO = Buffer.from("iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mNkYPhfDwAChwGA60e6kgAAAABJRU5ErkJggg==", "base64");
const EXPORTED = { timeout: 30_000 } as const;

test.describe("the brand in a Word document", { tag: ["@auth", "@desktop"] }, () => {
  test("a document carries the organization's logo, name and footer line, and a page without a brand only its name", async ({ api }, testInfo) => {
    test.slow();
    const key = uniqueKey(testInfo);
    const space = await createSpace(api, key, "Branded handbook");
    try {
      const guide = await createPage(api, space.homePageId, "Branded guide", {
        type: "doc",
        content: [{ type: "paragraph", content: [{ type: "text", text: "Written for Word." }] }],
      });
      const word = async () => {
        const { response, data } = await api.GET("/pages/{pageID}/docx", { params: { path: { pageID: guide.id } }, parseAs: "arrayBuffer" });
        expect(response.status).toBe(200);
        return unzip(Buffer.from(data as ArrayBuffer));
      };

      const plain = await word();
      expect(plain.get("word/header1.xml")?.toString()).not.toContain("<w:drawing>");
      expect(plain.has("word/media/brandlogo.png")).toBe(false);

      must(await api.PUT("/org/brand/footer", { body: { en: "Internal use only", de: "Nur intern" } }));
      const form = new FormData();
      form.append("file", new Blob([new Uint8Array(LOGO)], { type: "image/png" }), "logo.png");
      must(await api.PUT("/org/brand/logo", { body: form as unknown as { file: string } }));

      await expect
        .poll(async () => {
          const parts = await word();
          return parts.get("word/header1.xml")?.toString().includes("<w:drawing>") && parts.get("word/footer1.xml")?.toString().includes("Internal use only");
        }, EXPORTED)
        .toBe(true);
      const branded = await word();
      expect(branded.has("word/media/brandlogo.png")).toBe(true);
      expect(branded.get("word/document.xml")?.toString()).toContain("w:headerReference");
    } finally {
      await deleteSpace(api, key);
    }
  });
});
