import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { act, render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { Editor, type EditorHandle } from "./Editor";
import type { Doc, DocNode, Mentionable, MentionSource } from "./schema";
import { slashItemsFor } from "./slashItems";

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
      { id: "0199a1b2-c3d4-7e5f-8a9b-0c1d2e3f4a01", name: "Ada Lovelace", email: "ada@example.test" },
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

  it("look people up once the typing settles, and mark who cannot see the page", async () => {
    const user = userEvent.setup();
    const onChange = vi.fn();
    const everybody: Mentionable[] = [
      { id: "0199a1b2-c3d4-7e5f-8a9b-0c1d2e3f4a01", name: "Grace Hopper", email: "grace@example.test", canView: true },
      { id: "0199a1b2-c3d4-7e5f-8a9b-0c1d2e3f4a02", name: "Greta Garbo", email: "greta@example.test", canView: false },
    ];
    const source = vi.fn<MentionSource>(async (query) => everybody.filter((p) => p.name.toLowerCase().startsWith(query)));
    render(<Editor id="page-body" value={null} onChange={onChange} mentionSource={source} />);
    const box = document.getElementById("page-body")!;
    await user.click(box);
    await user.keyboard("@gr");
    const list = await screen.findByRole("listbox", { name: "People to mention" });
    await waitFor(() => expect(within(list).getAllByRole("option")).toHaveLength(2));
    expect(source).toHaveBeenCalledTimes(1);
    expect(source.mock.calls[0]![0]).toBe("gr");
    const greta = within(list).getByRole("option", { name: /Greta Garbo/ });
    expect(greta).toHaveTextContent("cannot see this page");
    expect(greta).toHaveAttribute("data-cannot-view");
    expect(within(list).getByRole("option", { name: /Grace Hopper/ })).not.toHaveTextContent("cannot see this page");

    await user.keyboard("e");
    await waitFor(() => expect(within(list).getAllByRole("option")).toHaveLength(1));
    await waitFor(() => expect(source).toHaveBeenCalledTimes(2));
    await user.keyboard("{Enter}");
    expect(find(onChange.mock.calls.at(-1)?.[0], "mention")[0]?.attrs).toMatchObject({ id: everybody[1]!.id, label: "Greta Garbo" });
  });

  it("drop a lookup whose answer comes after the list closed", async () => {
    const user = userEvent.setup();
    let signal: AbortSignal | undefined;
    const source = vi.fn<MentionSource>(
      (_query, s) =>
        new Promise((resolve) => {
          signal = s;
          s.addEventListener("abort", () => resolve([]));
        }),
    );
    render(<Editor id="page-body" value={null} onChange={vi.fn()} mentionSource={source} />);
    const box = document.getElementById("page-body")!;
    await user.click(box);
    await user.keyboard("@g");
    await waitFor(() => expect(source).toHaveBeenCalled());
    await user.keyboard("{Escape}");
    await waitFor(() => expect(signal?.aborted).toBe(true));
    expect(screen.queryByRole("listbox")).toBeNull();
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
    // Without an Armature to ask, its blocks are not offered.
    expect(options).toHaveLength(slashItemsFor(false).length);
    expect(within(list).queryByText("Armature issue")).toBeNull();
    for (const item of slashItemsFor(false)) {
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

describe("expand blocks", () => {
  it("insert from the slash menu with the caret in the title, which Enter leaves for the blocks inside", async () => {
    const { user, box, last } = setup();
    await slash(user, box, "expand");
    await user.keyboard("{Enter}");
    const title = await screen.findByRole("textbox", { name: "Expand title" });
    await waitFor(() => expect(title).toHaveFocus());
    await user.keyboard("Rollback steps{Enter}");
    expect(find(last(), "expand")[0]?.attrs?.title).toBe("Rollback steps");
    await waitFor(() => expect(box).toHaveFocus());
    await user.keyboard("Revert the release");
    const [block] = find(last(), "expand");
    expect(find(block, "text").map((n) => n.text)).toEqual(["Revert the release"]);
    await user.click(await screen.findByRole("button", { name: "Remove the expand, keep its content" }));
    expect(find(last(), "expand")).toHaveLength(0);
    expect(box).toHaveTextContent("Revert the release");
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
    expect(within(bar).getByRole("button", { name: "Find and replace" })).toHaveFocus();
    await user.keyboard("{ArrowRight}");
    expect(first).toHaveFocus();
    await user.keyboard("{ArrowLeft}");
    expect(within(bar).getByRole("button", { name: "Find and replace" })).toHaveFocus();
    expect(stops()).toEqual([within(bar).getByRole("button", { name: "Find and replace" })]);
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

describe("inside the page's form", () => {
  it("lets the toolbar and the heading link be used without submitting it", async () => {
    const user = userEvent.setup();
    const onChange = vi.fn();
    const submitted = vi.fn((event: SubmitEvent) => event.preventDefault());
    render(
      <form onSubmit={(event) => submitted(event.nativeEvent as SubmitEvent)}>
        <Editor id="page-body" value={null} onChange={onChange} />
      </form>,
    );
    const box = document.getElementById("page-body")!;
    const last = () => onChange.mock.calls.at(-1)?.[0] as Doc | undefined;

    await slash(user, box, "table");
    await user.keyboard("{Enter}");
    const tools = await screen.findByRole("toolbar", { name: "Table" });
    await user.click(within(tools).getByRole("button", { name: "Add a row below" }));
    expect(find(last(), "tableRow")).toHaveLength(4);
    await user.click(within(tools).getByRole("button", { name: "Delete the table" }));

    await slash(user, box, "h2");
    await user.keyboard("{Enter}");
    await user.keyboard("Release plan");
    await user.click(await screen.findByRole("button", { name: "Copy link to heading" }));
    expect(await screen.findByRole("status")).toHaveTextContent("Link copied");
    await user.click(within(screen.getByRole("toolbar", { name: "Formatting" })).getByRole("button", { name: "Bold" }));
    expect(screen.getByRole("button", { name: "Bold" })).toHaveAttribute("aria-pressed", "true");

    expect(submitted).not.toHaveBeenCalled();
  });
});

// jsdom draws nothing, so what is checked is which option is asked into view.
describe("the lists under the caret", () => {
  const scrolled = vi.fn();
  beforeEach(() => {
    Element.prototype.scrollIntoView = function (this: Element) {
      scrolled(this);
    };
  });
  afterEach(() => {
    delete (Element.prototype as Partial<Element>).scrollIntoView;
    scrolled.mockReset();
  });

  it("keep the slash menu's active option in view while focus stays in the editor", async () => {
    const { user, box } = setup();
    const list = await slash(user, box, "");
    const options = within(list).getAllByRole("option");
    await user.keyboard("{ArrowUp}");
    await waitFor(() => expect(scrolled).toHaveBeenLastCalledWith(options.at(-1)));
    expect(box).toHaveAttribute("aria-activedescendant", options.at(-1)!.id);
    expect(document.activeElement).toBe(box);
    expect(list).not.toHaveAttribute("tabindex");
    expect(list.className).toContain("overflow-y-hidden");
  });

  it("keep the mention list's active option in view while focus stays in the editor", async () => {
    const user = userEvent.setup();
    const people = [
      { id: "0199a1b2-c3d4-7e5f-8a9b-0c1d2e3f4a01", name: "Ada Lovelace" },
      { id: "u2", name: "Alan Turing" },
    ];
    render(<Editor id="page-body" value={null} onChange={vi.fn()} people={people} />);
    const box = document.getElementById("page-body")!;
    await user.click(box);
    await user.keyboard("@a");
    const list = await screen.findByRole("listbox", { name: "People to mention" });
    const options = within(list).getAllByRole("option");
    await user.keyboard("{ArrowDown}");
    await waitFor(() => expect(scrolled).toHaveBeenLastCalledWith(options[1]));
    expect(box).toHaveAttribute("aria-activedescendant", options[1]!.id);
    expect(document.activeElement).toBe(box);
    expect(list).not.toHaveAttribute("tabindex");
    expect(list.className).toContain("overflow-y-hidden");
  });
});
