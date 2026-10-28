import { afterEach, describe, expect, it, vi } from "vitest";
import { cleanup, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import type { ReactNode } from "react";
import { allowlist, problems } from "@/test/allowlist";
import { axeViolations } from "@/test/axe";
import { DIAGRAM_DEFAULT_SOURCE, DIAGRAM_KROKI_LANGUAGES, DIAGRAM_STARTERS } from "@/config";
import { Editor } from "./Editor";
import type { Doc } from "./schema";
import { sha256Hex } from "@/lib/sha256";
import { DocPageContext } from "./BlockViews";
import { DocView } from "./DocView";
import { krokiLanguage } from "./DiagramViews";
import { PublicLinkContext, PublicPageContext, PublicReadingContext } from "./publicReading";
import type { DocNode } from "./schema";

afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
});

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

describe("the diagram block", () => {
  const broken = "@startuml\nthis is not a diagram\n@enduml";

  // The api as Kroki answers it: a drawing as SVG, or why it cannot read the text.
  function stubKroki(languages: string[]) {
    const previews: { language: string; source: string }[] = [];
    vi.stubGlobal("fetch", async (request: Request) => {
      const path = new URL(request.url).pathname.replace(/^\/api\/v1/, "");
      const json = (status: number, body: unknown) => new Response(JSON.stringify(body), { status, headers: { "Content-Type": "application/json" } });
      if (path === "/meta") return json(200, { product: "stator", build: "test", apiVersion: "1", capabilities: [], diagramLanguages: languages });
      if (path === "/diagrams" && request.method === "POST") {
        const asked = (await request.json()) as { language: string; source: string };
        previews.push(asked);
        if (!languages.includes(asked.language)) {
          return json(422, { error: { code: "diagram_language_off", message: "This server draws Mermaid diagrams alone." } });
        }
        if (asked.source.includes("not a diagram")) {
          return json(422, { error: { code: "diagram_unreadable", message: "Syntax Error? (Assumed diagram type: sequence)." } });
        }
        return new Response(`<svg xmlns="http://www.w3.org/2000/svg"><text>${asked.language}</text></svg>`, {
          status: 200,
          headers: { "Content-Type": "image/svg+xml" },
        });
      }
      return json(404, { error: { code: "not_found", message: "Not stubbed." } });
    });
    return previews;
  }

  function setup(value: Doc) {
    const onChange = vi.fn();
    const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
    render(
      <QueryClientProvider client={client}>
        <main>
          <h1>Notes</h1>
          <Editor id="page-body" value={value} onChange={onChange} />
        </main>
      </QueryClientProvider>,
    );
    const box = document.getElementById("page-body")!;
    return { box, last: () => onChange.mock.calls.at(-1)?.[0] as Doc | undefined };
  }

  const mermaid: Doc = { type: "doc", content: [{ type: "diagram", attrs: { source: DIAGRAM_DEFAULT_SOURCE } }] };

  it("offers only the languages the server draws, and none to pick where it draws Mermaid alone", async () => {
    stubKroki(["mermaid"]);
    const { box } = setup(mermaid);
    await screen.findByLabelText("Diagram source in Mermaid");
    await new Promise((settle) => setTimeout(settle, 50));
    expect(within(box).queryByRole("combobox", { name: "Diagram language" })).toBeNull();
    cleanup();

    stubKroki(["mermaid", "plantuml", "graphviz"]);
    setup(mermaid);
    const picker = await screen.findByRole("combobox", { name: "Diagram language" });
    expect([...(picker as HTMLSelectElement).options].map((o) => o.textContent)).toEqual(["Mermaid", "PlantUML", "Graphviz"]);
  });

  it("previews a language Kroki draws as an image through the api, and says why broken text cannot be drawn", async () => {
    const previews = stubKroki(["mermaid", ...DIAGRAM_KROKI_LANGUAGES]);
    const { box, last } = setup(mermaid);
    fireEvent.change(await screen.findByRole("combobox", { name: "Diagram language" }), { target: { value: "plantuml" } });

    const source = await screen.findByLabelText("Diagram source in PlantUML");
    expect(source).toHaveValue(DIAGRAM_STARTERS.plantuml);
    const image = await waitFor(() => within(box).getByRole("img", { name: "Diagram" }));
    expect(image.getAttribute("src")).toMatch(/^data:image\/svg\+xml;charset=utf-8,/);
    expect(decodeURIComponent(image.getAttribute("src")!)).toContain("<text>plantuml</text>");
    // The drawing is an image, never part of the page.
    expect(box.querySelector("[data-diagram-preview] svg")).toBeNull();
    expect(previews.at(-1)).toEqual({ language: "plantuml", source: DIAGRAM_STARTERS.plantuml });
    await waitFor(() => expect(last()?.content?.[0]?.attrs).toEqual({ language: "plantuml", source: DIAGRAM_STARTERS.plantuml }));
    expect(problems(last()!)).toEqual([]);

    fireEvent.change(source, { target: { value: broken } });
    await waitFor(() =>
      expect(box.querySelector("[data-diagram-preview] [data-diagram-error]")).toHaveTextContent(
        "This diagram cannot be drawn: Syntax Error? (Assumed diagram type: sequence). Its source is shown instead.",
      ),
    );
    expect(box.querySelector("[data-diagram-preview] code")).toHaveTextContent("this is not a diagram");

    // Text of the author's own stays when the language changes.
    fireEvent.change(screen.getByRole("combobox", { name: "Diagram language" }), { target: { value: "d2" } });
    await waitFor(() => expect(last()?.content?.[0]?.attrs).toEqual({ language: "d2", source: broken }));
    fireEvent.change(screen.getByLabelText("Diagram source in D2"), { target: { value: " " } });
    await waitFor(() => expect(box.querySelector("[data-diagram-preview]")).toHaveTextContent("Write the diagram in D2."));
  });

  it("keeps a language this server does not draw, and says so", async () => {
    stubKroki(["mermaid"]);
    const { box } = setup({ type: "doc", content: [{ type: "diagram", attrs: { language: "structurizr", source: DIAGRAM_STARTERS.structurizr } }] });
    const picker = await screen.findByRole("combobox", { name: "Diagram language" });
    await waitFor(() => expect([...(picker as HTMLSelectElement).options].map((o) => o.value)).toEqual(["mermaid", "structurizr"]));
    expect(picker).toHaveValue("structurizr");
    await waitFor(() =>
      expect(box.querySelector("[data-diagram-preview] [data-diagram-error]")).toHaveTextContent("This server draws Mermaid diagrams alone."),
    );
  });

  it("passes axe with its picker and a drawing", async () => {
    stubKroki(["mermaid", ...DIAGRAM_KROKI_LANGUAGES]);
    const { box } = setup({ type: "doc", content: [{ type: "diagram", attrs: { language: "graphviz", source: DIAGRAM_STARTERS.graphviz } }] });
    await waitFor(() => within(box).getByRole("img", { name: "Diagram" }));
    await screen.findByRole("combobox", { name: "Diagram language" });
    expect(await axeViolations()).toEqual([]);
  });
});
