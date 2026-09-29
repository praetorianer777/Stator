import "@/test/layout";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { act, render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { Editor, type EditorHandle } from "./Editor";
import type { Doc, DocNode } from "./schema";
import { SLASH_ITEMS } from "./slashItems";

const written: Doc = { type: "doc", content: [{ type: "paragraph", content: [{ type: "text", text: "hello" }] }] };

function find(doc: DocNode | undefined, type: string): DocNode[] {
  if (!doc) return [];
  const out: DocNode[] = doc.type === type ? [doc] : [];
  for (const child of doc.content ?? []) out.push(...find(child, type));
  return out;
}

function setup(value: Doc | null = null) {
  const user = userEvent.setup();
  const onChange = vi.fn();
  render(<Editor id="page-body" value={value} onChange={onChange} />);
  const box = document.getElementById("page-body")!;
  const last = () => onChange.mock.calls.at(-1)?.[0] as Doc | undefined;
  return { user, onChange, box, last };
}

async function slash(user: ReturnType<typeof userEvent.setup>, box: HTMLElement, query: string) {
  await user.click(box);
  await user.type(box, `/${query}`);
  return screen.findByRole("listbox", { name: "Insert a block" });
}

beforeEach(() => localStorage.clear());

describe("Editor", () => {
  it("edits the document in place and reports every change as a document, or null when empty", async () => {
    const { user, box, last } = setup(written);
    expect(box.getAttribute("contenteditable")).toBe("true");
    expect(box.textContent).toBe("hello");
    box.focus();
    const range = document.createRange();
    range.selectNodeContents(box.querySelector("p")!);
    window.getSelection()!.removeAllRanges();
    window.getSelection()!.addRange(range);
    document.dispatchEvent(new Event("selectionchange"));
    await user.click(screen.getByRole("button", { name: "Bold" }));
    expect(JSON.stringify(last())).toContain('"bold"');
    expect(screen.getByRole("button", { name: "Bold" })).toHaveAttribute("aria-pressed", "true");
    await user.type(box, " there");
    expect(JSON.stringify(last())).toContain("there");
  });

  it("takes words from outside and can be emptied", () => {
    let handle: EditorHandle | undefined;
    const onChange = vi.fn();
    render(<Editor id="new-page" value={null} onChange={onChange} handle={(h) => (handle = h)} />);
    act(() => handle!.insertMarkdown("Thanks, **done**."));
    expect(document.getElementById("new-page")?.querySelector("strong")?.textContent).toBe("done");
    act(() => handle!.clear());
    expect(onChange).toHaveBeenLastCalledWith(null);
  });
});

describe("mentions", () => {
  it("offer the people given and insert the one picked", async () => {
    const user = userEvent.setup();
    const onChange = vi.fn();
    const people = [
      { id: "u1", name: "Ada Lovelace", email: "ada@example.test" },
      { id: "u2", name: "Grace Hopper" },
    ];
    render(<Editor id="page-body" value={null} onChange={onChange} people={people} />);
    const box = document.getElementById("page-body")!;
    await user.click(box);
    await user.keyboard("@gr");
    const list = await screen.findByRole("listbox", { name: "People to mention" });
    await waitFor(() => expect(within(list).getAllByRole("option")).toHaveLength(1));
    await user.keyboard("{Enter}");
    expect(find(onChange.mock.calls.at(-1)?.[0], "mention")[0]?.attrs).toMatchObject({ id: "u2", label: "Grace Hopper" });
  });

  it("offer nobody until a source of people is wired in", async () => {
    const { user, box } = setup();
    await user.click(box);
    await user.keyboard("@a");
    await new Promise((resolve) => setTimeout(resolve));
    expect(screen.queryByRole("listbox")).toBeNull();
  });
});

describe("the slash menu", () => {
  it("lists every block with its description and points the editor at the active one", async () => {
    const { user, box } = setup();
    const list = await slash(user, box, "");
    const options = within(list).getAllByRole("option");
    expect(options).toHaveLength(SLASH_ITEMS.length);
    for (const item of SLASH_ITEMS) {
      const option = within(list).getByText(item.label).closest('[role="option"]')!;
      expect(option).toHaveTextContent(item.description);
    }
    expect(box).toHaveAttribute("aria-controls", list.id);
    expect(box).toHaveAttribute("aria-activedescendant", options[0]!.id);
    expect(options[0]).toHaveAttribute("aria-selected", "true");
    await user.keyboard("{ArrowDown}");
    expect(box).toHaveAttribute("aria-activedescendant", options[1]!.id);
    await user.keyboard("{ArrowUp}{ArrowUp}");
    expect(box).toHaveAttribute("aria-activedescendant", options.at(-1)!.id);
  });

  it("filters by what is typed and inserts the pick with Enter", async () => {
    const { user, box, last } = setup();
    const list = await slash(user, box, "quo");
    await waitFor(() => expect(within(list).getAllByRole("option")).toHaveLength(1));
    await user.keyboard("{Enter}");
    await waitFor(() => expect(screen.queryByRole("listbox")).toBeNull());
    expect(find(last(), "blockquote")).toHaveLength(1);
    expect(JSON.stringify(last())).not.toContain("/quo");
    expect(box).not.toHaveAttribute("aria-controls");
  });

  it("says when nothing matches and closes on Escape", async () => {
    const { user, box } = setup();
    await slash(user, box, "");
    await user.type(box, "zzzz");
    expect(await screen.findByText("No block matches. Keep typing, or press Escape.")).toBeInTheDocument();
    await user.keyboard("{Escape}");
    await waitFor(() => expect(document.querySelector("[data-slash-menu]")).toBeNull());
    expect(box.textContent).toBe("/zzzz");
  });
});

describe("tables", () => {
  it("inserts one and edits rows, columns, headers and cell colours from its toolbar", async () => {
    const { user, box, last } = setup();
    await slash(user, box, "table");
    await user.keyboard("{Enter}");
    const tools = await screen.findByRole("toolbar", { name: "Table" });
    expect(find(last(), "tableRow")).toHaveLength(3);
    expect(find(last(), "tableHeader")).toHaveLength(3);

    await user.click(within(tools).getByRole("button", { name: "Add a row below" }));
    expect(find(last(), "tableRow")).toHaveLength(4);
    await user.click(within(tools).getByRole("button", { name: "Add a column after" }));
    expect(find(last(), "tableRow")[0]?.content).toHaveLength(4);
    await user.click(within(tools).getByRole("button", { name: "Delete the column" }));
    expect(find(last(), "tableRow")[0]?.content).toHaveLength(3);
    await user.click(within(tools).getByRole("button", { name: "Header row" }));
    expect(find(last(), "tableHeader")).toHaveLength(0);
    await user.click(within(tools).getByRole("button", { name: "Header column" }));
    expect(find(last(), "tableHeader")).toHaveLength(4);

    await user.click(within(tools).getByRole("button", { name: "Cell background" }));
    await user.click(await screen.findByRole("menuitem", { name: "Warning" }));
    expect(find(last(), "tableHeader").some((cell) => cell.attrs?.background === "warning")).toBe(true);
    expect(box.querySelector('[data-background="warning"]')).not.toBeNull();

    await user.click(within(screen.getByRole("toolbar", { name: "Table" })).getByRole("button", { name: "Delete the table" }));
    expect(find(last(), "table")).toHaveLength(0);
    expect(screen.queryByRole("toolbar", { name: "Table" })).toBeNull();
  });
});

describe("code blocks", () => {
  it("take a language from the picker and highlight it", async () => {
    const { user, box, last } = setup();
    await slash(user, box, "code");
    await user.keyboard("{Enter}");
    await user.keyboard("def main(): return 1");
    const picker = await screen.findByRole("combobox", { name: "Language" });
    expect(picker).toHaveValue("");
    await user.selectOptions(picker, "Python");
    expect(find(last(), "codeBlock")[0]?.attrs?.language).toBe("python");
    await waitFor(() => expect(box.querySelector("pre .hljs-keyword")?.textContent).toBe("def"));
  });
});

describe("panels", () => {
  it("insert from the slash menu and change kind", async () => {
    const { user, box, last } = setup();
    await slash(user, box, "warning");
    await user.keyboard("{Enter}");
    expect(find(last(), "panel")[0]?.attrs?.kind).toBe("warning");
    await user.keyboard("Mind the gap");
    const kind = await screen.findByRole("combobox", { name: "Panel type" });
    await user.selectOptions(kind, "Error");
    expect(find(last(), "panel")[0]?.attrs?.kind).toBe("error");
    expect(box.querySelector('[data-panel="error"]')).toHaveTextContent("Mind the gap");
    await user.click(screen.getByRole("button", { name: "Remove the panel" }));
    expect(find(last(), "panel")).toHaveLength(0);
  });
});

describe("heading anchors", () => {
  it("are saved with the heading and copied as a link", async () => {
    const { user, box, last } = setup();
    await slash(user, box, "h2");
    await user.keyboard("{Enter}");
    await user.keyboard("Release plan");
    expect(find(last(), "heading")[0]?.attrs).toMatchObject({ level: 2, id: "release-plan" });
    await user.click(await screen.findByRole("button", { name: "Copy link to heading" }));
    expect(await navigator.clipboard.readText()).toBe(`${window.location.origin}${window.location.pathname}#release-plan`);
    expect(await screen.findByRole("status")).toHaveTextContent("Link copied");
  });
});

describe("the toolbar", () => {
  it("is one Tab stop, moved through with the arrow keys", async () => {
    const { user } = setup(written);
    const bar = screen.getByRole("toolbar", { name: "Formatting" });
    const stops = () => Array.from(bar.querySelectorAll("button")).filter((b) => b.getAttribute("tabindex") === "0");
    expect(stops()).toHaveLength(1);
    const first = within(bar).getByRole("button", { name: "Text style: Text" });
    first.focus();
    await user.keyboard("{ArrowRight}");
    expect(within(bar).getByRole("button", { name: "Bold" })).toHaveFocus();
    await user.keyboard("{End}");
    expect(within(bar).getByRole("button", { name: "Panel" })).toHaveFocus();
    await user.keyboard("{ArrowRight}");
    expect(first).toHaveFocus();
    await user.keyboard("{ArrowLeft}");
    expect(within(bar).getByRole("button", { name: "Panel" })).toHaveFocus();
    expect(stops()).toEqual([within(bar).getByRole("button", { name: "Panel" })]);
  });

  it("sets a link only to a safe address", async () => {
    const { user, box, last } = setup(written);
    box.focus();
    const range = document.createRange();
    range.selectNodeContents(box.querySelector("p")!);
    window.getSelection()!.removeAllRanges();
    window.getSelection()!.addRange(range);
    document.dispatchEvent(new Event("selectionchange"));
    await user.click(screen.getByRole("button", { name: "Link" }));
    const address = screen.getByLabelText("Link address");
    await user.type(address, "javascript:alert(1)");
    expect(screen.getByRole("button", { name: "Set link" })).toBeDisabled();
    expect(screen.getByText(/Enter an address starting with/)).toBeInTheDocument();
    await user.clear(address);
    await user.type(address, "/spaces/eng{Enter}");
    const link = find(last(), "text")[0]?.marks?.find((m) => m.type === "link");
    expect(link?.attrs?.href).toBe("/spaces/eng");
  });
});
