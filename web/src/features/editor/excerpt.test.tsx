import { afterEach, describe, expect, it, vi } from "vitest";
import { cleanup, render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { Editor as TiptapEditor } from "@tiptap/core";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import type { ReactNode } from "react";
import { problems } from "@/test/allowlist";
import { stubApi } from "@/test/app";
import { axeViolations } from "@/test/axe";
import { aSpace } from "@/test/spaces";
import { ExcerptPicker, type IncludeChoice } from "@/features/pages/ExcerptPicker";
import { DocView } from "./DocView";
import { Editor } from "./Editor";
import { excerptName, freshName } from "./excerpt";
import { editorExtensions } from "./extensions";
import type { Doc, DocNode } from "./schema";

afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
});

function find(doc: DocNode | undefined, type: string): DocNode[] {
  if (!doc) return [];
  const out: DocNode[] = doc.type === type ? [doc] : [];
  for (const child of doc.content ?? []) out.push(...find(child, type));
  return out;
}

const ID_A = "0199a1b2-c3d4-7e5f-8a9b-0c1d2e3f4a71";
const ID_B = "0199a1b2-c3d4-7e5f-8a9b-0c1d2e3f4a72";
const line = (text: string): DocNode => ({ type: "paragraph", content: [{ type: "text", text }] });
const excerpt = (id: string, name: string, ...blocks: DocNode[]): DocNode => ({ type: "excerpt", attrs: { id, name }, content: blocks });

describe("excerpt names", () => {
  it("are some words cut at the API's limit, and a stock one is the first nobody has", () => {
    expect(excerptName("  ")).toBeNull();
    expect(excerptName("x".repeat(90))).toHaveLength(80);
    expect(freshName(new Set(["excerpt 1", "excerpt 3"]))).toBe("Excerpt 2");
  });
});

describe("the editor keeps excerpts as the server takes them", () => {
  function make(content: Doc) {
    return new TiptapEditor({ element: document.createElement("div"), extensions: editorExtensions(), content });
  }

  it("gives a pasted copy an id and a name of its own, and keeps the original's", () => {
    const e = make({ type: "doc", content: [excerpt(ID_A, "Hours", line("Nine to five")), { type: "paragraph" }] });
    e.commands.insertContentAt(e.state.doc.content.size, excerpt(ID_A, "Hours", line("Copied")));
    const found = find(e.getJSON() as DocNode, "excerpt");
    expect(found).toHaveLength(2);
    expect(found[0]?.attrs).toEqual({ id: ID_A, name: "Hours" });
    expect(found[1]?.attrs?.id).not.toBe(ID_A);
    expect(found[1]?.attrs?.name).toBe("Excerpt 1");
    expect(problems(e.getJSON() as DocNode)).toEqual([]);
    e.destroy();
  });

  it("takes the frame off an excerpt put inside another, and keeps its blocks", () => {
    const e = make({ type: "doc", content: [excerpt(ID_A, "Outer", line("first"))] });
    e.commands.insertContentAt(e.state.doc.content.size - 1, excerpt(ID_B, "Inner", line("second")));
    const found = find(e.getJSON() as DocNode, "excerpt");
    expect(found).toHaveLength(1);
    expect(find(found[0], "text").map((n) => n.text)).toEqual(["first", "second"]);
    expect(problems(e.getJSON() as DocNode)).toEqual([]);
    e.destroy();
  });

  it("is not made inside another excerpt", () => {
    const e = make({ type: "doc", content: [excerpt(ID_A, "Outer", line("first"))] });
    e.commands.setTextSelection(3);
    expect(e.commands.setExcerpt()).toBe(false);
    e.destroy();
  });
});

describe("the excerpt block", () => {
  it("comes from the slash menu with a stock name to type over, and keeps a name while the box is emptied", async () => {
    const user = userEvent.setup();
    const onChange = vi.fn();
    render(<Editor id="page-body" value={null} onChange={onChange} />);
    const box = document.getElementById("page-body")!;
    const last = () => onChange.mock.calls.at(-1)?.[0] as Doc | undefined;
    await user.click(box);
    await user.type(box, "Support hours /excerpt");
    await screen.findByRole("listbox", { name: "Insert a block" });
    await user.keyboard("{Enter}");
    const name = await screen.findByLabelText("Excerpt");
    await waitFor(() => expect(name).toHaveFocus());
    expect(name).toHaveValue("Excerpt 1");
    await user.clear(name);
    expect(find(last(), "excerpt")[0]?.attrs?.name).toBe("Excerpt 1");
    await user.type(name, "Support hours");
    await waitFor(() => expect(find(last(), "excerpt")[0]?.attrs?.name).toBe("Support hours"));
    expect(find(find(last(), "excerpt")[0], "text")[0]?.text?.trim()).toBe("Support hours");
    expect(problems(last()!)).toEqual([]);
  });
});

describe("the read-only view", () => {
  it("shows an excerpt as its blocks, without its name", () => {
    const { container } = render(<DocView doc={{ type: "doc", content: [excerpt(ID_A, "Hours", line("Nine to five"))] }} />);
    expect(container.querySelector(`[data-excerpt="${ID_A}"] p`)).toHaveTextContent("Nine to five");
    expect(container).not.toHaveTextContent("Hours");
  });
});

describe("the excerpt picker", () => {
  function withQueries(children: ReactNode) {
    return <QueryClientProvider client={new QueryClient({ defaultOptions: { queries: { retry: false } } })}>{children}</QueryClientProvider>;
  }
  const outline = [
    { id: "p-home", title: "Handbook", depth: 0, parentId: null },
    { id: "p-hours", title: "Support", depth: 1, parentId: "p-home" },
    { id: "p-self", title: "This page", depth: 1, parentId: "p-home" },
  ];

  it("lists a page's excerpts by name, leaves out the page being edited, and names the choice", async () => {
    stubApi({
      "GET /spaces": { status: 200, body: { spaces: [aSpace({ key: "DOCS", name: "Docs" })] } },
      "GET /spaces/DOCS/outline": { status: 200, body: { pages: outline } },
      "GET /pages/p-home/excerpts": { status: 200, body: { excerpts: [] } },
      "GET /pages/p-hours/excerpts": { status: 200, body: { excerpts: [{ id: ID_A, name: "Hours", text: "Nine to five on weekdays" }] } },
    });
    const user = userEvent.setup();
    const onPick = vi.fn<(choice: IncludeChoice) => void>();
    render(withQueries(<ExcerptPicker initialSpaceKey="DOCS" excludePageId="p-self" onPick={onPick} onClose={() => undefined} />));
    const dialog = await screen.findByRole("dialog", { name: "Choose what to include" });
    const page = within(dialog).getByLabelText("Page");
    await waitFor(() =>
      expect(
        within(page)
          .getAllByRole("option")
          .map((o) => o.textContent?.trim()),
      ).toEqual(["Handbook", "Support"]),
    );
    expect(await within(dialog).findByText(/This page has no excerpts/)).toBeInTheDocument();
    await user.selectOptions(page, "p-hours");
    const hours = await within(dialog).findByRole("radio", { name: /Hours/ });
    expect(within(dialog).getByText("Nine to five on weekdays")).toBeInTheDocument();
    expect(within(dialog).getByRole("radio", { name: "The whole page" })).toBeChecked();
    expect(await axeViolations({ popupOpen: true })).toEqual([]);
    await user.click(hours);
    await user.click(within(dialog).getByRole("button", { name: "Include" }));
    expect(onPick).toHaveBeenCalledWith({ pageId: "p-hours", pageTitle: "Support", spaceKey: "DOCS", excerptId: ID_A, excerptName: "Hours" });
  });
});
