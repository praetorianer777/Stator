import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import { describe, expect, it, vi } from "vitest";
import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { Editor } from "@tiptap/core";
import * as Y from "yjs";
import { editorExtensions } from "@/features/editor/extensions";
import { SKETCH_ASSET_PATH, SKETCH_DRAWING_MAX_LENGTH, SKETCH_MAX_ELEMENTS, SKETCH_SCENE_MAX_LENGTH, SKETCH_TITLE_MAX_LENGTH } from "@/config";
import { allowlist } from "@/test/allowlist";
import { DRAWING_RULES, cleanDrawing, drawingSrc } from "./drawing";
import { SketchLink, keepShared, sketchMap } from "./live";
import { settleSketches } from "./settle";
import { SketchFigure } from "./SketchViews";
import { emptyScene, hasDrawing, keepScene, readScene, sketchTitle, type SketchElement } from "./scene";

const drawScene = vi.fn();
vi.mock("./excalidraw", () => ({ drawScene: (...args: unknown[]) => drawScene(...args) }));

const ROOT = resolve(__dirname, "../../../..");

// As Excalidraw's exportToSvg writes one, before it is cleaned.
const EXPORTED = `<svg version="1.1" xmlns="http://www.w3.org/2000/svg" viewBox="0 0 120 70" width="120" height="70"><!-- svg-source:excalidraw --><metadata></metadata><defs><style class="style-fonts">
      @font-face { font-family: Excalifont; src: url(data:font/woff2;base64,d09GMgABAAAAA==); }</style></defs><rect x="0" y="0" width="120" height="70" fill="#ffffff"></rect><g stroke-linecap="round" transform="translate(10 10) rotate(0 50 25)"><path d="M0 0 L100 50" stroke="#1e1e1e" stroke-width="2" fill="none"></path></g><a href="https://example.test"><g transform="translate(10 10)"><text x="40" y="17" font-family="Excalifont, Xiaolai" font-size="20px" fill="#1e1e1e" text-anchor="middle" style="white-space: pre;" direction="ltr" dominant-baseline="alphabetic">Hello &lt;b&gt;</text></g></a><mask id="mask-a"><rect fill="#fff" width="10" height="10"></rect></mask><g mask="url(#mask-a)"></g></svg>`;

describe("a sketch's limits and rules", () => {
  it("match the API's", () => {
    const attrs = allowlist.nodes.sketch?.attrs ?? {};
    expect(attrs.scene?.maxLength).toBe(SKETCH_SCENE_MAX_LENGTH);
    expect(attrs.drawing?.maxLength).toBe(SKETCH_DRAWING_MAX_LENGTH);
    expect(attrs.title?.maxLength).toBe(SKETCH_TITLE_MAX_LENGTH);
    const rules = JSON.parse(readFileSync(resolve(ROOT, "api/sketch-drawing-allowlist.json"), "utf8"));
    expect(rules).toEqual(JSON.parse(JSON.stringify(DRAWING_RULES)));
    expect(SKETCH_MAX_ELEMENTS).toBe(5000);
  });

  it("puts Excalidraw's fonts where the build writes them", () => {
    const config = readFileSync(resolve(ROOT, "web/vite.config.ts"), "utf8");
    expect(config).toContain(`"${SKETCH_ASSET_PATH}fonts/"`);
  });
});

describe("cleanDrawing", () => {
  it("keeps what Excalidraw draws, in a form the API takes", () => {
    const out = cleanDrawing(EXPORTED);
    expect(out).not.toBeNull();
    expect(out).toContain('xmlns="http://www.w3.org/2000/svg"');
    expect(out).toContain("@font-face { font-family: Excalifont; src: url(data:font/woff2;base64,d09GMgABAAAAA==); }");
    expect(out).toContain("Hello &lt;b&gt;");
    expect(out).toContain('mask="url(#mask-a)"');
    expect(out).not.toContain("<a");
    expect(out).not.toContain("example.test");
    expect(out).not.toContain("metadata");
    expect(out).not.toContain("<!--");
  });

  it("takes out what could run or fetch", () => {
    const hostile = `<svg xmlns="http://www.w3.org/2000/svg" onload="alert(1)"><script>alert(1)</script><foreignObject><div xmlns="http://www.w3.org/1999/xhtml">x</div></foreignObject><image href="https://x.test/a.png"/><rect fill="url(https://x.test/p.svg#a)" onclick="alert(1)" width="1"/><rect style="background: u\\72l(x)"/><style>rect { fill: red }</style><style>@font-face { font-family: A; src: url(https://esm.sh/a.woff2); }</style><g>stray words</g><use href="#a"/></svg>`;
    const out = cleanDrawing(hostile) ?? "";
    for (const gone of ["onload", "script", "foreignObject", "image", "x.test", "onclick", "style", "fill: red", "esm.sh", "stray words", "use"]) {
      expect(out, gone).not.toContain(gone);
    }
    expect(out).toContain('<rect width="1"/>');
  });

  it("answers null for what is no SVG, or too large to keep", () => {
    expect(cleanDrawing("<html><body/></html>")).toBeNull();
    expect(cleanDrawing("<svg")).toBeNull();
    expect(cleanDrawing(`<svg xmlns="http://www.w3.org/2000/svg"><text>${"x".repeat(SKETCH_DRAWING_MAX_LENGTH)}</text></svg>`)).toBeNull();
  });

  it("shows a drawing as a picture's address", () => {
    expect(drawingSrc('<svg a="#"/>')).toBe("data:image/svg+xml;charset=utf-8,%3Csvg%20a%3D%22%23%22%2F%3E");
  });
});

describe("the scene a sketch keeps", () => {
  const box: SketchElement = { id: "b1", type: "rectangle", x: 0, y: 0, link: null };

  it("keeps the shapes on the canvas and its background, nothing else", () => {
    const kept = keepScene(
      [
        box,
        { id: "gone", type: "ellipse", isDeleted: true },
        { id: "l1", type: "arrow", link: "javascript:alert(1)", customData: { a: 1 } },
        { id: "l2", type: "text", text: "Hi", link: "https://example.test/a" },
      ],
      { viewBackgroundColor: "#fafafa", zoom: { value: 2 }, theme: "dark" },
    );
    expect(kept.leftOut).toBe(false);
    expect(kept.tooLarge).toBe(false);
    expect(JSON.parse(kept.scene)).toEqual({
      elements: [box, { id: "l1", type: "arrow", link: null }, { id: "l2", type: "text", text: "Hi", link: "https://example.test/a" }],
      appState: { viewBackgroundColor: "#fafafa" },
    });
  });

  it("leaves out pictures and embedded sites, and says so", () => {
    const kept = keepScene([box, { id: "i1", type: "image", fileId: "f" }, { id: "e1", type: "embeddable", link: "https://example.test" }], {});
    expect(kept.leftOut).toBe(true);
    expect(JSON.parse(kept.scene).elements).toEqual([box]);
  });

  it("says when it is too large to keep", () => {
    const many = Array.from({ length: SKETCH_MAX_ELEMENTS + 1 }, (_, i) => ({ ...box, id: `b${i}` }));
    expect(keepScene(many, {}).tooLarge).toBe(true);
    expect(keepScene([{ ...box, text: "x".repeat(SKETCH_SCENE_MAX_LENGTH) }], {}).tooLarge).toBe(true);
  });

  it("reads a stored scene back, and nothing else", () => {
    expect(readScene(emptyScene())).toEqual({ elements: [], appState: { viewBackgroundColor: "#ffffff" } });
    expect(readScene('{"elements":[{"id":"a","type":"line"}]}')?.appState.viewBackgroundColor).toBe("#ffffff");
    for (const bad of [null, 3, "not json", "[]", '{"elements":{}}', '{"elements":[{"type":"line"}]}']) expect(readScene(bad)).toBeNull();
    expect(hasDrawing(readScene(emptyScene()))).toBe(false);
    expect(hasDrawing(readScene('{"elements":[{"id":"a","type":"line","isDeleted":true}]}'))).toBe(false);
    expect(hasDrawing(readScene('{"elements":[{"id":"a","type":"line"}]}'))).toBe(true);
    expect(sketchTitle("  Plan  ")).toBe("Plan");
    expect(sketchTitle(" ")).toBeNull();
    expect(sketchTitle("x".repeat(SKETCH_TITLE_MAX_LENGTH + 5))).toHaveLength(SKETCH_TITLE_MAX_LENGTH);
  });
});

describe("SketchFigure", () => {
  const drawing = `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 10 10"><rect width="10" height="10"/></svg>`;
  const scene = '{"elements":[{"id":"a","type":"rectangle"}],"appState":{"viewBackgroundColor":"#ffffff"}}';

  it("shows the saved drawing as a picture named by its title, and opens it larger", async () => {
    drawScene.mockReset();
    render(<SketchFigure attrs={{ scene, drawing, title: "Payment flow" }} />);
    const picture = screen.getByRole("img", { name: "Sketch: Payment flow" });
    expect(picture.getAttribute("src")).toBe(drawingSrc(drawing));
    expect(screen.getByText("Payment flow").tagName).toBe("FIGCAPTION");
    await userEvent.click(screen.getByRole("button", { name: "View the sketch Payment flow larger" }));
    expect(screen.getByRole("dialog")).toBeInTheDocument();
    expect(drawScene).not.toHaveBeenCalled();
  });

  it("names a sketch without a title as one", () => {
    render(<SketchFigure attrs={{ scene, drawing, title: null }} />);
    expect(screen.getByRole("img", { name: "Sketch without a title" })).toBeInTheDocument();
    expect(screen.getByRole("figure", { name: "Sketch without a title" })).toBeInTheDocument();
  });

  it("draws a sketch saved without its drawing, busy until it is drawn", async () => {
    drawScene.mockReset();
    let finish: (svg: string) => void = () => {};
    drawScene.mockReturnValue(new Promise<string>((done) => (finish = done)));
    const { container } = render(<SketchFigure attrs={{ scene, drawing: null, title: "Imported" }} />);
    expect(container.querySelector('[data-sketch-state="drawing"]')).not.toBeNull();
    finish(drawing);
    expect(await screen.findByRole("img", { name: "Sketch: Imported" })).toBeInTheDocument();
    expect(drawScene).toHaveBeenCalledWith(readScene(scene));
  });

  it("says when it cannot be drawn, and shows nothing for an empty sketch", async () => {
    drawScene.mockReset();
    drawScene.mockRejectedValue(new Error("offline"));
    render(<SketchFigure attrs={{ scene, drawing: null, title: null }} />);
    expect(await screen.findByText(/cannot be drawn here/)).toBeInTheDocument();
    const { container } = render(<SketchFigure attrs={{ scene: emptyScene(), drawing: null, title: "Empty" }} />);
    expect(container).toBeEmptyDOMElement();
  });
});

describe("settleSketches", () => {
  const SKETCH = "5f0c6a2e-9a8b-4c1d-8e2f-0a1b2c3d4e5f";
  const box = (version: number) => ({ id: "box", type: "rectangle", x: 0, y: 0, width: 10, height: 10, index: "a0", version, versionNonce: 1 });
  const scene = (version: number) => JSON.stringify({ elements: [box(version)], appState: { viewBackgroundColor: "#ffffff" } });

  it("puts what is drawn together into the page, and leaves sketches that are current or never drawn together", async () => {
    drawScene.mockReset();
    drawScene.mockResolvedValue('<svg xmlns="http://www.w3.org/2000/svg"><rect width="2"/></svg>');
    const doc = new Y.Doc();
    const shapes = [box(3)];
    new SketchLink(doc, SKETCH, { elements: () => shapes, background: () => "#ffffff", apply: () => {} }, 0);
    const editor = new Editor({
      element: document.createElement("div"),
      extensions: editorExtensions(),
      content: {
        type: "doc",
        content: [
          { type: "sketch", attrs: { scene: scene(1), drawing: "<svg/>", title: "Drawn together", sketchId: SKETCH } },
          { type: "sketch", attrs: { scene: scene(1), drawing: "<svg/>", title: "Alone", sketchId: null } },
        ],
      },
    });
    expect(await settleSketches(editor, doc)).toBe(true);
    const [together, alone] = (editor.getJSON().content ?? []).map((node) => node.attrs ?? {});
    expect(together?.scene).toBe(keepShared(sketchMap(doc, SKETCH))?.scene);
    expect(readScene(together?.scene)?.elements[0]?.version).toBe(3);
    expect(together?.drawing).toBe('<svg xmlns="http://www.w3.org/2000/svg"><rect width="2"/></svg>');
    expect(together?.title).toBe("Drawn together");
    expect(alone?.scene).toBe(scene(1));
    expect(drawScene).toHaveBeenCalledTimes(1);

    expect(await settleSketches(editor, doc)).toBe(false);
    expect(drawScene).toHaveBeenCalledTimes(1);
    editor.destroy();
  });
});
