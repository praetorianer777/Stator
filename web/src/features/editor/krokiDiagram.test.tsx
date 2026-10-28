import { afterEach, describe, expect, it } from "vitest";
import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import type { ReactNode } from "react";
import { allowlist } from "@/test/allowlist";
import { DIAGRAM_KROKI_LANGUAGES } from "@/config";
import { sha256Hex } from "@/lib/sha256";
import { DocPageContext } from "./BlockViews";
import { DocView } from "./DocView";
import { krokiLanguage } from "./DiagramViews";
import { PublicLinkContext, PublicPageContext, PublicReadingContext } from "./publicReading";
import type { DocNode } from "./schema";

afterEach(cleanup);

const PAGE = "6f1c2b9e-0d4a-4c51-9a77-1f0e3b2d4c5a";
const source = "@startuml\nAlice -> Bob: hello\n@enduml";
const hash = sha256Hex(`plantuml\n${source}`);
const doc: DocNode = { type: "doc", content: [{ type: "diagram", attrs: { language: "plantuml", source } }] };

function shown(wrap: (children: ReactNode) => ReactNode) {
  render(wrap(<DocView doc={doc} />));
  return screen.getByRole("img", { name: "Diagram" });
}

describe("a diagram Kroki draws", () => {
  it("names a language the api draws, and Mermaid for any other", () => {
    expect(DIAGRAM_KROKI_LANGUAGES.map(krokiLanguage)).toEqual(["plantuml", "structurizr", "graphviz", "d2"]);
    expect([undefined, "mermaid", "bpmn", 3].map(krokiLanguage)).toEqual([null, null, null, null]);
    expect(allowlist.nodes.diagram?.attrs?.language?.enum).toEqual(["mermaid", ...DIAGRAM_KROKI_LANGUAGES]);
  });

  it("is an image of the page's diagram by its hash, wherever the page is read", () => {
    expect(shown((c) => <DocPageContext value={{ id: PAGE, spaceKey: "DOCS" }}>{c}</DocPageContext>).getAttribute("src")).toBe(
      `/api/v1/pages/${PAGE}/diagrams/${hash}`,
    );
    cleanup();
    expect(
      shown((c) => (
        <PublicReadingContext value={true}>
          <PublicPageContext value={PAGE}>{c}</PublicPageContext>
        </PublicReadingContext>
      )).getAttribute("src"),
    ).toBe(`/api/v1/public/pages/${PAGE}/diagrams/${hash}`);
    cleanup();
    expect(
      shown((c) => (
        <PublicReadingContext value={true}>
          <PublicLinkContext value="tok">{c}</PublicLinkContext>
        </PublicReadingContext>
      )).getAttribute("src"),
    ).toBe(`/api/v1/public/links/tok/diagrams/${hash}`);
  });

  it("holds the print while it is drawn, and shows its source saying why once it cannot be", () => {
    const image = shown((c) => <DocPageContext value={{ id: PAGE, spaceKey: "DOCS" }}>{c}</DocPageContext>);
    expect(document.querySelector('[data-diagram-state="drawing"]')).not.toBeNull();
    fireEvent.error(image);
    expect(document.querySelector('[data-diagram-state="drawing"]')).toBeNull();
    expect(screen.getByText("This diagram cannot be drawn right now. Its source is shown instead.")).toBeTruthy();
    expect(screen.getByText(/Alice -> Bob: hello/)).toBeTruthy();
  });

  it("stops holding the print once it is drawn", () => {
    fireEvent.load(shown((c) => <DocPageContext value={{ id: PAGE, spaceKey: "DOCS" }}>{c}</DocPageContext>));
    expect(document.querySelector('[data-diagram-state="drawing"]')).toBeNull();
    expect(screen.getByRole("img", { name: "Diagram" })).toBeTruthy();
  });
});

describe("the editor", () => {
  it("keeps a diagram's language through an edit, and writes Mermaid for a new one", async () => {
    const { Editor } = await import("@tiptap/core");
    const { editorExtensions } = await import("./extensions");
    const editor = new Editor({ element: document.createElement("div"), extensions: editorExtensions(), content: doc });
    editor.commands.insertContentAt(editor.state.doc.content.size, { type: "diagram", attrs: { source: "graph TD" } });
    const attrs = (editor.getJSON().content ?? []).filter((n) => n.type === "diagram").map((n) => n.attrs);
    expect(attrs).toEqual([
      { language: "plantuml", source },
      { language: "mermaid", source: "graph TD" },
    ]);
    editor.destroy();
  });
});
