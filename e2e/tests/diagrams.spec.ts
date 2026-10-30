import { readFile } from "node:fs/promises";
import type { Page, TestInfo } from "@playwright/test";
import type { StatorApi } from "../fixtures/api";
import { expect } from "../fixtures/auth";
import { caretTo } from "../fixtures/editor";
import { teamTest as test } from "../fixtures/team";
import { ONE_LOOK, openEditor, openUntil } from "../fixtures/replica";
import { uniqueName } from "../fixtures/seed";
import { expectAccessible, startInScheme, type ColourScheme } from "../fixtures/shell";
import { createPage, createSpace, deleteSpace, publishFromEditor, uniqueKey, type Space } from "../fixtures/spaces";

const editorBox = (page: Page) => page.locator("#page-body");
const shown = (page: Page) => page.locator("[data-doc]");

const paragraph = (value: string) => ({ type: "paragraph", content: [{ type: "text", text: value }] });

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
    const path = `/spaces/${space.key}/p/${notes.id}/architecture`;

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
    await openUntil(page, `/spaces/${space.key}/p/${notes.id}/hostile`, () =>
      expect(shown(page).locator("[data-diagram-svg] > svg")).toContainText("Plain", ONE_LOOK),
    );
    // A click on a node does nothing in strict mode, so its anchor has no address.
    await expect(
      shown(page).locator("[data-diagram-svg] img, [data-diagram-svg] foreignObject, [data-diagram-svg] [href], [data-diagram-svg] [onerror]"),
    ).toHaveCount(0);
    expect(alerted).toBe(false);
  });

  test("an author picks PlantUML, sees it drawn and why broken text is not, and a reader sees the drawing", async ({ page, api }, testInfo) => {
    const space = await freshSpace(api, testInfo, "Picked");
    const notes = await createPage(api, space.homePageId, "Sequence", { type: "doc", content: [paragraph("How a page is saved."), { type: "paragraph" }] });
    const word = `Picked${Date.now().toString(36)}`;

    await openEditor(page, `/spaces/${space.key}/p/${notes.id}/sequence`, "How a page is saved.");
    await caretTo(editorBox(page), "end");
    await page.keyboard.type("/diagram");
    await expect(page.getByRole("listbox", { name: "Insert a block" })).toBeVisible();
    await page.keyboard.press("Enter");

    const block = editorBox(page).locator("[data-diagram-edit]");
    const preview = block.locator("[data-diagram-preview]");
    await block.getByRole("combobox", { name: "Diagram language" }).selectOption({ label: "PlantUML" });
    const source = block.getByLabel("Diagram source in PlantUML");
    await expect(source).toHaveValue(/^@startuml/);
    const image = preview.locator("img[data-diagram-image]");
    await expect(image).toBeVisible();
    await expect.poll(() => image.evaluate((img: HTMLImageElement) => img.complete && img.naturalWidth > 0)).toBe(true);
    // Kroki's drawing stays an image; nothing it wrote is part of the page.
    await expect(preview.locator("svg")).toHaveCount(0);

    await source.fill("@startuml\nthis is not a diagram\n@enduml");
    await expect(preview.locator("[data-diagram-error]")).toContainText("This diagram cannot be drawn:");
    await expect(preview.locator("code")).toContainText("this is not a diagram");
    await source.fill(`@startuml\nAuthor -> Api: ${word}\n@enduml`);
    await expect.poll(() => image.getAttribute("src").then((src) => decodeURIComponent(src ?? ""))).toContain(word);
    await expect(preview.locator("[data-diagram-error]")).toHaveCount(0);

    await expect(page.locator("[data-draft-status]")).toHaveAttribute("data-draft-status", "saved");
    await publishFromEditor(page);

    const drawing = shown(page).locator('[data-diagram-language="plantuml"] img[data-diagram-image]');
    await expect(drawing).toBeVisible();
    await expect.poll(() => drawing.evaluate((img: HTMLImageElement) => img.complete && img.naturalWidth > 0)).toBe(true);
    await expect(shown(page).locator("[data-diagram-error]")).toHaveCount(0);
    const { data } = await api.GET("/pages/{pageID}", { params: { path: { pageID: notes.id } } });
    expect(JSON.stringify(data)).toContain(`"language":"plantuml"`);
  });

  test("a PlantUML diagram is drawn by the installation's Kroki, for an author, a reader and the print", async ({ page, api }, testInfo) => {
    const space = await freshSpace(api, testInfo, "Kroki");
    const word = `Kroki${Date.now().toString(36)}`;
    const source = `@startuml\nAlice -> Bob: ${word}\n@enduml`;

    const { data, response } = await api.POST("/diagrams", { body: { language: "plantuml", source }, parseAs: "text" });
    expect(response.status, String(data)).toBe(200);
    expect(response.headers.get("content-type")).toBe("image/svg+xml");
    expect(response.headers.get("content-security-policy")).toBe("default-src 'none'; style-src 'unsafe-inline'; img-src data:");
    expect(data).toContain(word);

    const unreadable = await api.POST("/diagrams", { body: { language: "plantuml", source: "@startuml\nthis is not a diagram\n@enduml" } });
    expect(unreadable.response.status).toBe(422);
    expect(unreadable.error).toMatchObject({ error: { code: "diagram_unreadable" } });

    const notes = await createPage(api, space.homePageId, "Sequence", { type: "doc", content: [{ type: "diagram", attrs: { language: "plantuml", source } }] });
    const drawing = (scope: Page) => scope.locator('[data-diagram-language="plantuml"] img[data-diagram-image]');
    const loaded = (scope: Page) => drawing(scope).evaluate((img: HTMLImageElement) => img.complete && img.naturalWidth > 0);
    await openUntil(page, `/spaces/${space.key}/p/${notes.id}/sequence`, () => expect(drawing(page)).toBeVisible(ONE_LOOK));
    await expect.poll(() => loaded(page)).toBe(true);
    await expect(shown(page).locator("[data-diagram-error]")).toHaveCount(0);

    // The render service prints once the page says it is ready, which waits for the drawing.
    await page.goto(`/print/p/${notes.id}`);
    await expect(page.locator("html")).toHaveAttribute("data-print-ready", "", { timeout: 30_000 });
    expect(await loaded(page)).toBe(true);
    await expect(page.locator('[data-diagram-state="drawing"]')).toHaveCount(0);
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
      await openUntil(page, `/spaces/${space.key}/p/${notes.id}/notes`, () =>
        expect(shown(page).locator("[data-diagram-svg] > svg")).toContainText("Save the page", ONE_LOOK),
      );
      await expect(shown(page).locator("[data-diagram-error]")).toContainText("This diagram cannot be drawn");
      await expectAccessible(page);
    });

    test(`the diagram block passes axe in ${scheme}, with Kroki's drawing and its error`, async ({ page, api }, testInfo) => {
      const space = await freshSpace(api, testInfo, "Block");
      const notes = await createPage(api, space.homePageId, "Block", {
        type: "doc",
        content: [
          paragraph("Two diagrams."),
          { type: "diagram", attrs: { language: "plantuml", source: "@startuml\nAda -> Stator: Save the page\n@enduml" } },
          { type: "diagram", attrs: { language: "graphviz", source: "digraph {" } },
        ],
      });
      await startInScheme(page, scheme);
      await openEditor(page, `/spaces/${space.key}/p/${notes.id}/block`, "Two diagrams.");
      const blocks = editorBox(page).locator("[data-diagram-edit]");
      await expect(blocks.first().locator("[data-diagram-preview] img[data-diagram-image]")).toBeVisible();
      await expect(blocks.nth(1).locator("[data-diagram-preview] [data-diagram-error]")).toContainText("This diagram cannot be drawn:");
      await expect(blocks.first().getByRole("combobox", { name: "Diagram language" })).toHaveValue("plantuml");
      await expectAccessible(page);
    });
  }
});
