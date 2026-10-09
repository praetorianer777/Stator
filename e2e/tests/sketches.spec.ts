import type { Locator, Page, TestInfo } from "@playwright/test";
import { must, type StatorApi } from "../fixtures/api";
import { expect } from "../fixtures/auth";
import { withDatabase } from "../fixtures/db";
import { caretTo } from "../fixtures/editor";
import { orgTest as test } from "../fixtures/org";
import { ONE_LOOK, openEditor, openUntil } from "../fixtures/replica";
import { uniqueName } from "../fixtures/seed";
import { expectAccessible, startInScheme, type ColourScheme } from "../fixtures/shell";
import { createPage, createSpace, deleteSpace, publishFromEditor, uniqueKey, type Space } from "../fixtures/spaces";
import { WEB_URL } from "../fixtures/stack";

const ANONYMOUS = { cookies: [], origins: [] };

const editorBox = (page: Page) => page.locator("#page-body");
const shown = (page: Page) => page.locator("[data-doc]");
const sketchDialog = (page: Page) => page.locator("[data-sketch-dialog]");

const paragraph = (value: string) => ({ type: "paragraph", content: [{ type: "text", text: value }] });

// A drawing as the editor saves one, small enough to write here.
const LEDGER_DRAWING =
  '<svg version="1.1" xmlns="http://www.w3.org/2000/svg" viewBox="0 0 220 120" width="220" height="120"><rect x="0" y="0" width="220" height="120" fill="#ffffff"></rect>' +
  '<g stroke-linecap="round" transform="translate(16 16)"><path d="M0 0 L188 0 L188 88 L0 88 Z" stroke="#1e1e1e" stroke-width="2" fill="none"></path></g>' +
  '<g transform="translate(110 66)"><text x="0" y="0" font-family="Excalifont, Xiaolai, Segoe UI Emoji" font-size="20px" fill="#1e1e1e" text-anchor="middle" style="white-space: pre;">Ledger</text></g></svg>';
const LEDGER_SCENE = JSON.stringify({
  elements: [
    { id: "ledger-box", type: "rectangle", x: 0, y: 0, width: 188, height: 88, strokeColor: "#1e1e1e", backgroundColor: "transparent" },
    { id: "ledger-text", type: "text", x: 60, y: 30, width: 70, height: 25, text: "Ledger", originalText: "Ledger", fontSize: 20, fontFamily: 5 },
  ],
  appState: { viewBackgroundColor: "#ffffff" },
});
const sketch = (title: string | null, drawing: string | null = LEDGER_DRAWING) => ({ type: "sketch", attrs: { scene: LEDGER_SCENE, drawing, title } });

/** How many pixels of a canvas around a point, given from its middle in CSS pixels, are ink: dark ones, or any that are drawn at all. */
async function inkAt(canvas: Locator, dx: number, dy: number, kind: "dark" | "drawn", half = 40): Promise<number> {
  return canvas.evaluate(
    (el, { dx, dy, kind, half }) => {
      const c = el as HTMLCanvasElement;
      const scale = c.width / c.clientWidth;
      const x = Math.round((c.clientWidth / 2 + dx - half) * scale);
      const y = Math.round((c.clientHeight / 2 + dy - half) * scale);
      const size = Math.round(2 * half * scale);
      const { data } = c.getContext("2d")!.getImageData(x, y, size, size);
      let ink = 0;
      for (let i = 0; i < data.length; i += 4) {
        const [r, g, b, a] = [data[i]!, data[i + 1]!, data[i + 2]!, data[i + 3]!];
        if (kind === "drawn" ? a > 0 : a > 0 && r + g + b < 300) ink++;
      }
      return ink;
    },
    { dx, dy, kind, half },
  );
}

/** A sketch's canvas, and where on it a point from its middle is. */
async function canvasOf(page: Page) {
  const dialog = sketchDialog(page);
  const interactive = dialog.locator("canvas.interactive");
  await expect(interactive).toBeVisible();
  const box = (await interactive.boundingBox())!;
  return {
    dialog,
    interactive,
    drawn: dialog.locator("canvas.excalidraw__canvas:not(.interactive)"),
    at: (dx: number, dy: number) => ({ x: box.x + box.width / 2 + dx, y: box.y + box.height / 2 + dy }),
  };
}

/** Writes words on a canvas where a point from its middle is. */
async function writeAt(page: Page, at: { x: number; y: number }, words: string) {
  await page.keyboard.press("t");
  await page.mouse.click(at.x, at.y);
  await page.keyboard.type(words);
  await page.keyboard.press("Escape");
}

/** Whether the shared draft this browser keeps has words in it, as they reach it from another browser. */
async function sharedDraftHolds(page: Page, words: string): Promise<boolean> {
  return page.evaluate(async (words) => {
    for (const { name } of await indexedDB.databases()) {
      if (!name?.startsWith("stator.collab.")) continue;
      const db = await new Promise<IDBDatabase>((resolve, reject) => {
        const opening = indexedDB.open(name);
        opening.onsuccess = () => resolve(opening.result);
        opening.onerror = () => reject(opening.error);
      });
      const updates = await new Promise<Uint8Array[]>((resolve, reject) => {
        const reading = db.transaction("updates").objectStore("updates").getAll();
        reading.onsuccess = () => resolve(reading.result as Uint8Array[]);
        reading.onerror = () => reject(reading.error);
      });
      db.close();
      if (updates.some((update) => new TextDecoder().decode(update).includes(words))) return true;
    }
    return false;
  }, words);
}

/** Every request a page makes to a host other than Stator's own. */
function requestsElsewhere(page: Page): string[] {
  const origin = new URL(WEB_URL).origin;
  const elsewhere: string[] = [];
  page.on("request", (request) => {
    const url = new URL(request.url());
    if (url.protocol !== "data:" && url.protocol !== "blob:" && url.origin !== origin) elsewhere.push(request.url());
  });
  return elsewhere;
}

/** The SVG a sketch's picture shows. */
async function drawingOf(picture: Locator): Promise<string> {
  const src = (await picture.getAttribute("src")) ?? "";
  expect(src).toMatch(/^data:image\/svg\+xml;charset=utf-8,/);
  return decodeURIComponent(src.slice(src.indexOf(",") + 1));
}

test.describe("sketches", { tag: ["@auth"] }, () => {
  const made: string[] = [];
  test.afterEach(async ({ api }) => {
    for (const key of made.splice(0)) await deleteSpace(api, key);
  });

  async function freshSpace(api: StatorApi, testInfo: TestInfo, label: string): Promise<Space> {
    const key = uniqueKey(testInfo);
    made.push(key);
    return createSpace(api, key, uniqueName(testInfo, label));
  }

  test("an author draws a box with words, and readers see it drawn, larger, and in history", { tag: ["@desktop"] }, async ({ page, api }, testInfo) => {
    test.slow();
    const elsewhere = requestsElsewhere(page);
    const space = await freshSpace(api, testInfo, "Sketches");
    const notes = await createPage(api, space.homePageId, "Request flow", {
      type: "doc",
      content: [paragraph("How a request travels."), { type: "paragraph" }],
    });
    const path = `/s/${space.key}/p/${notes.id}/request-flow`;

    await openEditor(page, path, "How a request travels.");
    await caretTo(editorBox(page), "end");
    await page.keyboard.type("/sketch");
    await expect(page.getByRole("listbox", { name: "Insert a block" })).toBeVisible();
    await page.keyboard.press("Enter");

    // The canvas opens by itself for a new sketch.
    const dialog = sketchDialog(page);
    await expect(dialog.getByRole("heading", { name: "Sketch" })).toBeVisible();
    const canvas = dialog.locator("canvas.interactive");
    await expect(canvas).toBeVisible();
    const box = (await canvas.boundingBox())!;
    const at = (dx: number, dy: number) => ({ x: box.x + box.width / 2 + dx, y: box.y + box.height / 2 + dy });

    // R picks the rectangle; a drag draws it, and Enter on it writes words in it.
    await page.mouse.click(at(-200, -150).x, at(-200, -150).y);
    await page.keyboard.press("r");
    await page.mouse.move(at(-120, -60).x, at(-120, -60).y);
    await page.mouse.down();
    await page.mouse.move(at(0, 0).x, at(0, 0).y, { steps: 5 });
    await page.mouse.move(at(120, 60).x, at(120, 60).y, { steps: 5 });
    await page.mouse.up();
    await page.keyboard.press("Enter");
    await page.keyboard.type("Gateway");
    // Escape inside the canvas ends the words, and leaves the dialog open.
    await page.keyboard.press("Escape");
    await expect(dialog).toBeVisible();
    await dialog.getByRole("button", { name: "Done" }).click();
    await expect(dialog).toHaveCount(0);

    const block = editorBox(page).locator("[data-sketch-edit]");
    const preview = block.getByRole("img", { name: "Sketch without a title" });
    await expect(preview).toBeVisible();
    const drawn = await drawingOf(preview);
    expect(drawn).toContain(">Gateway</text>");
    expect(drawn).toMatch(/@font-face \{ font-family: Excalifont; src: url\(data:font\/woff2;base64,/);
    expect(drawn).not.toContain("<script");

    await block.getByLabel("Title").fill("Request flow");
    await expect(block.getByRole("img", { name: "Sketch: Request flow" })).toBeVisible();
    await expect(page.locator("[data-draft-status]")).toHaveAttribute("data-draft-status", "saved");
    await publishFromEditor(page);

    const figure = shown(page).locator("[data-sketch]");
    const picture = figure.getByRole("img", { name: "Sketch: Request flow" });
    await expect(picture).toBeVisible();
    expect(await drawingOf(picture)).toContain(">Gateway</text>");
    await expect(figure.locator("figcaption")).toHaveText("Request flow");

    await figure.getByRole("button", { name: "View the sketch Request flow larger" }).click();
    const lightbox = page.getByRole("dialog", { name: "Request flow" });
    await expect(lightbox.getByRole("img", { name: "Sketch: Request flow" })).toBeVisible();
    await page.keyboard.press("Escape");
    await expect(lightbox).toHaveCount(0);

    await page.goto(`${path}/history?from=1&to=2`);
    const compared = page.locator("[data-diff-view]");
    await expect(compared.locator('[data-diff-block="inserted"]').getByRole("img", { name: "Sketch: Request flow" })).toBeVisible();

    expect(elsewhere, "requests that left Stator's origin").toEqual([]);
  });

  test(
    "two people draw on one sketch at once, see each other's shapes and pointers, merge what was drawn offline, and publish it",
    { tag: ["@desktop"] },
    async ({ page, api, apiAs, pageAs }, testInfo) => {
      test.slow();
      const space = await freshSpace(api, testInfo, "Drawn together");
      const notes = await createPage(api, space.homePageId, "Whiteboard", {
        type: "doc",
        content: [
          paragraph("Our whiteboard."),
          { type: "sketch", attrs: { scene: JSON.stringify({ elements: [], appState: { viewBackgroundColor: "#ffffff" } }), drawing: null, title: "Board" } },
        ],
      });
      const aliceName = must(await api.GET("/auth/me")).user.name;
      const bobName = must(await (await apiAs("bob")).GET("/auth/me")).user.name;
      const path = `/s/${space.key}/p/${notes.id}/whiteboard`;
      const elsewhere = requestsElsewhere(page);

      await openEditor(page, path, "Our whiteboard.");
      await expect(page.locator("[data-page-editor]")).toHaveAttribute("data-collab", "together");
      const bob = await pageAs("bob");
      await openUntil(bob, `${path}/edit`, () => expect(bob.locator("[data-page-editor]")).toHaveAttribute("data-collab", "together", ONE_LOOK));

      // Alice opens the sketch first, which gives it its place in the shared draft.
      await editorBox(page).getByRole("button", { name: "Edit sketch" }).click();
      const alice = await canvasOf(page);
      await expect(alice.dialog).toHaveAttribute("data-sketch-live", "");
      await expect(alice.dialog.getByRole("button", { name: "Cancel" })).toHaveCount(0);
      // Bob sees in the page that she is drawing on it, and joins her.
      await expect(editorBox(bob).getByRole("list", { name: `Drawing on this sketch now: ${aliceName}` })).toBeVisible();
      await editorBox(bob).getByRole("button", { name: "Edit sketch" }).click();
      const bobs = await canvasOf(bob);
      await expect(alice.dialog.getByRole("list", { name: `Drawing on this sketch now: ${bobName}` })).toBeVisible();
      await expect(bobs.dialog.getByRole("list", { name: `Drawing on this sketch now: ${aliceName}` })).toBeVisible();

      // Alice's box with words, drawn at the left, appears on Bob's canvas without anybody leaving it.
      expect(await inkAt(bobs.drawn, -200, -80, "dark")).toBe(0);
      await page.mouse.click(alice.at(0, 200).x, alice.at(0, 200).y);
      await page.keyboard.press("r");
      await page.mouse.move(alice.at(-260, -130).x, alice.at(-260, -130).y);
      await page.mouse.down();
      await page.mouse.move(alice.at(-200, -80).x, alice.at(-200, -80).y, { steps: 4 });
      await page.mouse.move(alice.at(-140, -30).x, alice.at(-140, -30).y, { steps: 4 });
      await page.mouse.up();
      await page.keyboard.press("Enter");
      await page.keyboard.type("Alpha");
      await page.keyboard.press("Escape");
      await expect.poll(() => inkAt(bobs.drawn, -200, -80, "dark")).toBeGreaterThan(50);

      // Bob's words at the right appear on Alice's, at the same time as she points somewhere else.
      await bob.mouse.click(bobs.at(0, 200).x, bobs.at(0, 200).y);
      await writeAt(bob, bobs.at(140, -80), "Bravo");
      await expect.poll(() => inkAt(alice.drawn, 170, -70, "dark")).toBeGreaterThan(20);

      // Each sees where the other's pointer is, in their colour.
      await page.keyboard.press("v");
      expect(await inkAt(bobs.interactive, 160, 160, "drawn", 30)).toBe(0);
      await page.mouse.move(alice.at(150, 150).x, alice.at(150, 150).y, { steps: 3 });
      await expect.poll(() => inkAt(bobs.interactive, 160, 160, "drawn", 30)).toBeGreaterThan(20);
      await bob.mouse.move(bobs.at(-150, 150).x, bobs.at(-150, 150).y, { steps: 3 });
      await expect.poll(() => inkAt(alice.interactive, -140, 160, "drawn", 30)).toBeGreaterThan(20);

      // Bob loses the connection and keeps drawing; it reaches Alice once he is back.
      await bob.context().setOffline(true);
      await expect(alice.dialog.getByRole("list", { name: `Drawing on this sketch now: ${bobName}` })).toHaveCount(0);
      await writeAt(bob, bobs.at(-40, 60), "Charlie");
      await expect.poll(() => inkAt(bobs.drawn, -10, 70, "dark")).toBeGreaterThan(20);
      expect(await inkAt(alice.drawn, -10, 70, "dark")).toBe(0);
      await bob.context().setOffline(false);
      await expect.poll(() => inkAt(alice.drawn, -10, 70, "dark"), { timeout: 15_000 }).toBeGreaterThan(20);

      // Alice leaves the canvas, which puts the drawing in the page.
      await alice.dialog.getByRole("button", { name: "Done" }).click();
      await expect(alice.dialog).toHaveCount(0);
      const preview = editorBox(page).getByRole("img", { name: "Sketch: Board" });
      await expect.poll(async () => drawingOf(preview)).toContain(">Charlie</text>");
      const left = await drawingOf(preview);
      for (const words of ["Alpha", "Bravo", "Charlie"]) expect(left).toContain(`>${words}</text>`);

      // Bob is still drawing when Alice publishes; what he drew is published too.
      await bob.mouse.click(bobs.at(0, 200).x, bobs.at(0, 200).y);
      await writeAt(bob, bobs.at(100, 60), "Delta");
      await expect(editorBox(page).getByRole("list", { name: `Drawing on this sketch now: ${bobName}` })).toBeVisible();
      await expect(page.locator("[data-draft-status]")).toHaveAttribute("data-draft-status", "saved");
      await expect.poll(() => sharedDraftHolds(page, "Delta")).toBe(true);
      await publishFromEditor(page);
      const published = shown(page).getByRole("img", { name: "Sketch: Board" });
      await expect(published).toBeVisible();
      const drawn = await drawingOf(published);
      for (const words of ["Alpha", "Bravo", "Charlie", "Delta"]) expect(drawn).toContain(`>${words}</text>`);

      // Readers see the drawing, never the canvas.
      const reader = await pageAs("bob");
      const readerElsewhere = requestsElsewhere(reader);
      await openUntil(reader, path, () => expect(shown(reader).getByRole("img", { name: "Sketch: Board" })).toBeVisible(ONE_LOOK));
      expect(await drawingOf(shown(reader).getByRole("img", { name: "Sketch: Board" }))).toContain(">Delta</text>");
      await expect(reader.locator("canvas")).toHaveCount(0);
      expect([...elsewhere, ...readerElsewhere], "requests that left Stator's origin").toEqual([]);
    },
  );

  test("the canvas is reached and left by keyboard, and leaving it unchanged keeps the sketch as it was", async ({ page, api }, testInfo) => {
    test.slow();
    const elsewhere = requestsElsewhere(page);
    const space = await freshSpace(api, testInfo, "Keyboard");
    const notes = await createPage(api, space.homePageId, "Ledger", { type: "doc", content: [sketch("Ledger")] });
    const path = `/s/${space.key}/p/${notes.id}/ledger`;

    await openUntil(page, `${path}/edit`, () => expect(editorBox(page).getByRole("img", { name: "Sketch: Ledger" })).toBeVisible(ONE_LOOK));
    const before = await drawingOf(editorBox(page).getByRole("img", { name: "Sketch: Ledger" }));
    expect(before).toContain(">Ledger</text>");
    const edit = editorBox(page).getByRole("button", { name: "Edit sketch" });
    await edit.focus();
    await page.keyboard.press("Enter");
    const dialog = sketchDialog(page);
    await expect(dialog.locator("canvas.interactive")).toBeVisible();
    // The dialog fits the window it opens in, a phone's included.
    const viewport = page.viewportSize()!;
    const frame = (await dialog.boundingBox())!;
    expect(frame.width).toBeLessThanOrEqual(viewport.width);
    await expect(dialog.getByRole("button", { name: "Done" })).toBeInViewport();

    // Out of the canvas, Escape leaves it, and the keyboard is back where it was.
    // Drawn together there is nothing to cancel; nothing was drawn, so nothing changed.
    await dialog.getByRole("button", { name: "Done" }).focus();
    await page.keyboard.press("Escape");
    await expect(dialog).toHaveCount(0);
    await expect(edit).toBeFocused();
    expect(await drawingOf(editorBox(page).getByRole("img", { name: "Sketch: Ledger" }))).toBe(before);
    expect(elsewhere, "requests that left Stator's origin").toEqual([]);
  });

  test("anybody reads a sketch from the public view, a public link and the print view, an imported one drawn on the spot", async ({
    api,
    browser,
    page,
  }, testInfo) => {
    test.slow();
    const space = await freshSpace(api, testInfo, "Open sketches");
    const guide = await createPage(api, space.homePageId, "Open ledger", { type: "doc", content: [sketch("Ledger"), sketch("Imported ledger", null)] });
    must(await api.PUT("/spaces/{spaceKey}/anonymous-access", { params: { path: { spaceKey: space.key } }, body: { view: true } }));
    must(await api.PUT("/org/anonymous-access", { body: { enabled: true, indexable: false } }));
    const context = await browser.newContext({ baseURL: WEB_URL, storageState: ANONYMOUS });
    try {
      const org = must(await api.GET("/auth/me")).organization!.slug;
      const reader = await context.newPage();
      const elsewhere = requestsElsewhere(reader);
      const pictures = (on: Page) => on.locator("[data-doc] [data-sketch] img");

      await openUntil(reader, `/public/${org}/s/${space.key}/p/${guide.id}/open-ledger`, () => expect(pictures(reader)).toHaveCount(2, ONE_LOOK));
      await expect(reader.getByRole("img", { name: "Sketch: Ledger" })).toBeVisible();
      const imported = reader.getByRole("img", { name: "Sketch: Imported ledger" });
      await expect(imported).toBeVisible();
      expect(await drawingOf(imported)).toContain(">Ledger</text>");

      const link = must(await api.POST("/pages/{pageID}/public-links", { params: { path: { pageID: guide.id } }, body: {} }));
      await openUntil(reader, link.path, () => expect(pictures(reader)).toHaveCount(2, ONE_LOOK));
      expect(elsewhere, "requests that left Stator's origin").toEqual([]);

      // The render service prints once the page says it is ready, which waits for every sketch to be drawn.
      await page.goto(`/print/p/${guide.id}`);
      await expect(page.locator("html")).toHaveAttribute("data-print-ready", "", { timeout: 30_000 });
      await expect(page.locator("[data-sketch] img[data-sketch-image]")).toHaveCount(2);
      await expect(page.locator('[data-sketch-state="drawing"]')).toHaveCount(0);
    } finally {
      await context.close();
      must(await api.PUT("/org/anonymous-access", { body: { enabled: false, indexable: false } }));
    }
  });

  test("a drawing planted past the API shows cleaned, as a picture that runs and fetches nothing", async ({ page, api }, testInfo) => {
    const space = await freshSpace(api, testInfo, "Planted");
    const notes = await createPage(api, space.homePageId, "Planted", { type: "doc", content: [sketch("Planted")] });
    const hostile =
      '<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 10 10" onload="window.sketchRan = 1"><script>window.sketchRan = 2</script>' +
      '<image href="https://elsewhere.test/a.png" width="10" height="10"/><foreignObject><div xmlns="http://www.w3.org/1999/xhtml">x</div></foreignObject><rect width="10" height="10" fill="url(https://elsewhere.test/p.svg#a)"/></svg>';
    await withDatabase(async (db) => {
      const { rowCount } = await db.query("UPDATE page SET body = jsonb_set(body, '{content,0,attrs,drawing}', to_jsonb($2::text)) WHERE id = $1", [
        notes.id,
        hostile,
      ]);
      expect(rowCount).toBe(1);
    });
    const elsewhere = requestsElsewhere(page);
    await openUntil(page, `/s/${space.key}/p/${notes.id}/planted`, async () => {
      const shownDrawing = await drawingOf(shown(page).getByRole("img", { name: "Sketch: Planted" }));
      expect(shownDrawing).toContain('<rect width="10" height="10"/>');
    });
    const shownDrawing = await drawingOf(shown(page).getByRole("img", { name: "Sketch: Planted" }));
    for (const gone of ["onload", "script", "image", "foreignObject", "elsewhere.test"]) expect(shownDrawing).not.toContain(gone);
    expect(await page.evaluate(() => (window as unknown as { sketchRan?: number }).sketchRan)).toBeUndefined();
    expect(elsewhere, "requests that left Stator's origin").toEqual([]);
  });

  for (const scheme of ["light", "dark"] as ColourScheme[]) {
    test(`sketches pass axe in ${scheme}, in the page, the editor and the lightbox`, async ({ page, api }, testInfo) => {
      test.slow();
      const space = await freshSpace(api, testInfo, "Axe");
      const notes = await createPage(api, space.homePageId, "Notes", { type: "doc", content: [sketch("Ledger"), sketch(null)] });
      const path = `/s/${space.key}/p/${notes.id}/notes`;
      await startInScheme(page, scheme);
      await openUntil(page, path, () => expect(shown(page).getByRole("img", { name: "Sketch: Ledger" })).toBeVisible(ONE_LOOK));
      await expect(shown(page).getByRole("figure", { name: "Sketch without a title" })).toBeVisible();
      const width = page.viewportSize()!.width;
      for (const picture of await shown(page).locator("[data-sketch] img").all()) {
        expect((await picture.boundingBox())!.width).toBeLessThanOrEqual(width);
      }
      await expectAccessible(page);

      await shown(page).getByRole("button", { name: "View the sketch Ledger larger" }).click();
      await expect(page.getByRole("dialog", { name: "Ledger" })).toBeVisible();
      await expectAccessible(page);
      await page.keyboard.press("Escape");

      await openUntil(page, `${path}/edit`, () => expect(editorBox(page).getByRole("img", { name: "Sketch: Ledger" })).toBeVisible(ONE_LOOK));
      await expectAccessible(page);
    });
  }
});
