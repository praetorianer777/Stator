import { readFile } from "node:fs/promises";
import type { Page, TestInfo } from "@playwright/test";
import type { StatorApi } from "../fixtures/api";
import { expect } from "../fixtures/auth";
import { caretTo } from "../fixtures/editor";
import { orgTest as test } from "../fixtures/org";
import { uniqueName } from "../fixtures/seed";
import { expectAccessible, startInScheme, type ColourScheme } from "../fixtures/shell";
import { createPage, createSpace, deleteSpace, publishFromEditor, uniqueKey, type Space } from "../fixtures/spaces";

const editorBox = (page: Page) => page.locator("#page-body");
const shown = (page: Page) => page.locator("[data-doc]");

const paragraph = (value: string) => ({ type: "paragraph", content: [{ type: "text", text: value }] });

/** Opens a page's editor until it holds the words given, which a replica may lag behind on. */
async function openEditor(page: Page, path: string, words: string): Promise<void> {
  await expect(async () => {
    await page.goto(`${path}/edit`);
    await expect(editorBox(page)).toContainText(words, { timeout: 2_000 });
  }).toPass();
}

test.describe("diagrams", { tag: ["@auth"] }, () => {
  const made: string[] = [];
  test.afterEach(async ({ api }) => {
    for (const key of made.splice(0)) await deleteSpace(api, key);
  });

  async function freshSpace(api: StatorApi, testInfo: TestInfo, label: string): Promise<Space> {
    const key = uniqueKey(testInfo);
    made.push(key);
    return createSpace(api, key, uniqueName(testInfo, label));
  }

  test("an author writes a diagram and watches it drawn, and a reader saves it as SVG", async ({ page, api }, testInfo) => {
    const space = await freshSpace(api, testInfo, "Diagrams");
    // The empty paragraph at the end takes the block: an Enter after the caret
    // key would split wherever ProseMirror last saw the caret.
    const notes = await createPage(api, space.homePageId, "Architecture", { type: "doc", content: [paragraph("How requests flow."), { type: "paragraph" }] });
    const path = `/s/${space.key}/p/${notes.id}/architecture`;

    await openEditor(page, path, "How requests flow.");
    await caretTo(editorBox(page), "end");
    await page.keyboard.type("/diagram");
    await expect(page.getByRole("listbox", { name: "Insert a block" })).toBeVisible();
    await page.keyboard.press("Enter");

    const block = editorBox(page).locator("[data-diagram-edit]");
    const preview = block.locator("[data-diagram-preview]");
    await expect(preview.locator("[data-diagram-svg] > svg")).toContainText("Published");

    const source = block.getByLabel("Diagram source in Mermaid");
    await source.fill("flowchart LR\n  browser[Browser] -->");
    await expect(preview.locator("[data-diagram-error]")).toContainText("This diagram cannot be drawn");
    await source.fill("flowchart LR\n  browser[Browser] --> gateway[Gateway] --> db[(Postgres)]");
    await expect(preview.locator("[data-diagram-svg] > svg")).toContainText("Gateway");
    await expect(preview.locator("[data-diagram-error]")).toHaveCount(0);

    await expect(page.locator("[data-draft-status]")).toHaveAttribute("data-draft-status", "saved");
    await publishFromEditor(page);

    const figure = shown(page).locator("[data-diagram]");
    await expect(figure.locator("[data-diagram-svg] > svg")).toContainText("Postgres");
    const saving = page.waitForEvent("download");
    await figure.getByRole("button", { name: "Download as SVG" }).click();
    const file = await saving;
    expect(file.suggestedFilename()).toBe("diagram.svg");
    const svg = await readFile(await file.path(), "utf8");
    expect(svg).toMatch(/^<svg[^>]*xmlns="http:\/\/www.w3.org\/2000\/svg"/);
    expect(svg).toContain("Gateway");
  });

  test("a diagram's labels are text, never markup or a link", async ({ page, api }, testInfo) => {
    const space = await freshSpace(api, testInfo, "Hostile");
    const notes = await createPage(api, space.homePageId, "Hostile", {
      type: "doc",
      content: [
        {
          type: "diagram",
          attrs: { source: 'flowchart LR\n  a["<img src=x onerror=alert(1)>Plain"] --> b\n  click a "javascript:alert(2)"' },
        },
      ],
    });
    let alerted = false;
    page.on("dialog", async (dialog) => {
      alerted = true;
      await dialog.dismiss();
    });
    await expect(async () => {
      await page.goto(`/s/${space.key}/p/${notes.id}/hostile`);
      await expect(shown(page).locator("[data-diagram-svg] > svg")).toContainText("Plain", { timeout: 2_000 });
    }).toPass();
    // A click on a node does nothing in strict mode, so its anchor has no address.
    await expect(
      shown(page).locator("[data-diagram-svg] img, [data-diagram-svg] foreignObject, [data-diagram-svg] [href], [data-diagram-svg] [onerror]"),
    ).toHaveCount(0);
    expect(alerted).toBe(false);
  });

  for (const scheme of ["light", "dark"] as ColourScheme[]) {
    test(`diagrams pass axe in ${scheme}, a broken one too`, async ({ page, api }, testInfo) => {
      const space = await freshSpace(api, testInfo, "Axe");
      const notes = await createPage(api, space.homePageId, "Notes", {
        type: "doc",
        content: [
          { type: "diagram", attrs: { source: "sequenceDiagram\n  Ada->>Stator: Save the page\n  Stator-->>Ada: Saved" } },
          { type: "diagram", attrs: { source: "flowchart LR\n  a -->" } },
        ],
      });
      await startInScheme(page, scheme);
      await expect(async () => {
        await page.goto(`/s/${space.key}/p/${notes.id}/notes`);
        await expect(shown(page).locator("[data-diagram-svg] > svg")).toContainText("Save the page", { timeout: 2_000 });
      }).toPass();
      await expect(shown(page).locator("[data-diagram-error]")).toContainText("This diagram cannot be drawn");
      await expectAccessible(page);
    });
  }
});
