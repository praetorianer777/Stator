import { afterEach, describe, expect, it, vi } from "vitest";
import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { allowlist, problems } from "@/test/allowlist";
import { DIAGRAM_DEFAULT_SOURCE, DIAGRAM_MAX_LENGTH } from "@/config";
import { DocView } from "./DocView";
import { Editor } from "./Editor";
import { diagramSource, diagramTheme, drawDiagram } from "./DiagramViews";
import type { Doc, DocNode } from "./schema";
// Mermaid is large. Loaded inside a test it outlasted the test's time on a busy
// machine; loaded here it is at hand.
import "mermaid";

afterEach(cleanup);

function find(doc: DocNode | undefined, type: string): DocNode[] {
  if (!doc) return [];
  const out: DocNode[] = doc.type === type ? [doc] : [];
  for (const child of doc.content ?? []) out.push(...find(child, type));
  return out;
}

const broken = "flowchart LR\n  a --> ";

describe("diagram sources", () => {
  it("keep what the server takes, cut at its limit", () => {
    expect(diagramSource(" \n ")).toBeNull();
    expect(diagramSource(7)).toBeNull();
    expect(diagramSource("x".repeat(DIAGRAM_MAX_LENGTH + 3))).toHaveLength(DIAGRAM_MAX_LENGTH);
    expect(allowlist.nodes.diagram?.attrs?.source?.maxLength).toBe(DIAGRAM_MAX_LENGTH);
  });

  it("that Mermaid cannot read say where, and leave nothing behind in the page", async () => {
    const before = document.body.innerHTML;
    const out = await drawDiagram(broken);
    expect(out.svg).toBeNull();
    expect(out.error).toMatch(/^Parse error on line 3/);
    expect(document.body.innerHTML).toBe(before);
  });
});

describe("diagram colours", () => {
  const style = (tokens: Record<string, string>) => ({ getPropertyValue: (name: string) => tokens[name] ?? "" }) as unknown as CSSStyleDeclaration;
  const hex = {
    "--color-surface": "#fdfcfa",
    "--color-surface-raised": "#f1eee8",
    "--color-surface-sunken": "#ece8e1",
    "--color-border-strong": "#c9c2b6",
    "--color-accent-subtle": "#e6ecfa",
    "--color-ink": "#1f1b16",
    "--color-ink-muted": "#625b51",
  };

  it("follow the page's theme", () => {
    const got = diagramTheme(style(hex), true);
    expect(got.theme).toBe("base");
    expect(got.themeVariables).toMatchObject({ primaryTextColor: "#1f1b16", lineColor: "#625b51", darkMode: "true" });
  });

  it("fall back to Mermaid's own set for a theme Mermaid cannot read", () => {
    expect(diagramTheme(style({ ...hex, "--color-ink": "oklch(0.2 0 0)" }), true)).toEqual({ theme: "dark" });
    expect(diagramTheme(style({}), false)).toEqual({ theme: "default" });
  });
});

describe("the read-only view", () => {
  it("shows a diagram's source and why it cannot be drawn", async () => {
    const { container } = render(<DocView doc={{ type: "doc", content: [{ type: "diagram", attrs: { source: broken } }] }} />);
    expect(container.querySelector("[data-diagram] code")).toHaveTextContent("flowchart LR a -->");
    await waitFor(() => expect(container.querySelector("[data-diagram-error]")).toHaveTextContent(/This diagram cannot be drawn: Parse error on line 3/));
    expect(container.querySelector('[data-action="download-diagram"]')).toBeNull();
  });
});

describe("the diagram block", () => {
  function setup(value: Doc | null = null) {
    const user = userEvent.setup();
    const onChange = vi.fn();
    render(
      <main>
        <Editor id="page-body" value={value} onChange={onChange} />
      </main>,
    );
    const box = document.getElementById("page-body")!;
    return { user, box, last: () => onChange.mock.calls.at(-1)?.[0] as Doc | undefined };
  }

  it("comes from the slash menu with a sketch to start from, and keeps its source as it is typed", async () => {
    const { user, box, last } = setup();
    await user.click(box);
    await user.type(box, "/diagram");
    await screen.findByRole("listbox", { name: "Insert a block" });
    await user.keyboard("{Enter}");
    const source = await screen.findByLabelText("Diagram source in Mermaid");
    expect(source).toHaveValue(DIAGRAM_DEFAULT_SOURCE);
    fireEvent.change(source, { target: { value: broken } });
    await waitFor(() => expect(find(last(), "diagram")[0]?.attrs?.source).toBe(broken));
    expect(problems(last()!)).toEqual([]);
    // The preview says what is wrong once the typing stops.
    await waitFor(() => expect(box.querySelector("[data-diagram-preview] [data-diagram-error]")).toHaveTextContent(/Parse error on line 3/));
    fireEvent.change(source, { target: { value: "  " } });
    await waitFor(() => expect(box.querySelector("[data-diagram-preview]")).toHaveTextContent("Write the diagram in Mermaid"));
  });
});
