import { afterEach, beforeAll, describe, expect, it, vi } from "vitest";
import { act, render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { Editor } from "@tiptap/core";
import type { Comment, Thread } from "@/api/comments";
import { renderAt, stubApi, type Answer } from "@/test/app";
import { axeViolations } from "@/test/axe";
import { problems } from "@/test/allowlist";
import { aPage, aSpace } from "@/test/spaces";
import { DocView } from "@/features/editor/DocView";
import { editorExtensions } from "@/features/editor/extensions";
import { PassagesContext } from "@/features/editor/passages";
import type { Doc, DocMark, DocNode } from "@/features/editor/schema";
import { findPassage, markPassage, relocate, selectedPassage } from "./passages";

beforeAll(() => {
  // jsdom lays nothing out, so a range has no box of its own.
  Range.prototype.getBoundingClientRect ??= () => new DOMRect();
});

afterEach(() => {
  vi.unstubAllGlobals();
  vi.restoreAllMocks();
  window.getSelection()?.removeAllRanges();
});

const pageId = "0195f000-0000-7000-8000-0000000000e1";
const open = "0195f000-0000-7000-8000-00000000e0a1";
const gone = "0195f000-0000-7000-8000-00000000e0b1";
const settled = "0195f000-0000-7000-8000-00000000e0c1";

const anchor = (threadId: string) => ({ type: "inlineComment", attrs: { threadId } });
const text = (value: string, ...marks: DocMark[]): DocNode =>
  marks.length ? { type: "text", text: value, marks } : { type: "text", text: value };
const para = (...content: DocNode[]): DocNode => ({ type: "paragraph", content });
const doc = (...content: DocNode[]): Doc => ({ type: "doc", content });
const say = (value: string): Doc => doc(para(text(value)));

/** Every thread id marked in a document, with the text its mark covers. */
function marksIn(node: DocNode, out: Record<string, string> = {}): Record<string, string> {
  for (const mark of node.marks ?? []) {
    if (mark.type === "inlineComment") out[String(mark.attrs?.threadId)] = (out[String(mark.attrs?.threadId)] ?? "") + (node.text ?? "");
  }
  for (const child of node.content ?? []) marksIn(child, out);
  return out;
}

describe("a passage in a page's body", () => {
  const body = doc(
    para(text("We ship "), text("on Friday", { type: "bold" }), text(" with "), { type: "mention", attrs: { id: "u1", label: "Ada" } }, text(" after review.")),
    { type: "bulletList", content: [{ type: "listItem", content: [para(text("Friday again, twice Friday."))] }] },
  );

  it("is marked by splitting the text it covers, a mention inside it included, and nothing else changes", () => {
    const marked = markPassage(body, { path: [0], start: 3, end: 26 }, open);
    expect(marksIn(marked)).toEqual({ [open]: "ship on Friday with  af" });
    const inline = marked.content![0]!.content!;
    expect(inline.map((n) => n.text ?? `@${String(n.attrs?.label)}`)).toEqual(["We ", "ship ", "on Friday", " with ", "@Ada", " af", "ter review."]);
    expect(inline[2]!.marks).toEqual([{ type: "bold" }, anchor(open)]);
    expect(inline[4]!.marks).toEqual([anchor(open)]);
    expect(problems(marked)).toEqual([]);
    expect(body.content![0]!.content).toHaveLength(5);
  });

  it("is found again by its words only where they occur once", () => {
    expect(findPassage(body, "after review")).toEqual({ path: [0], start: 24, end: 36 });
    expect(findPassage(body, "twice")).toEqual({ path: [1, 0, 0], start: 14, end: 19 });
    expect(findPassage(body, "Friday")).toBeNull();
    expect(findPassage(body, "not there")).toBeNull();
    const moved = doc(para(text("First.")), ...body.content!);
    expect(relocate(moved, { path: [0], start: 24, end: 36, quote: "after review" })).toEqual({ path: [1], start: 24, end: 36 });
    expect(relocate(body, { path: [0], start: 24, end: 36, quote: "after review" })).toEqual({ path: [0], start: 24, end: 36, quote: "after review" });
  });

  it("is read from a selection in the reader, trimmed, within one block", () => {
    const { container } = render(
      <PassagesContext value={{ shown: new Set(), open: () => {} }}>
        <DocView doc={body} />
      </PassagesContext>,
    );
    const root = container.firstElementChild as HTMLElement;
    const first = root.querySelector('[data-block="0"]')!;
    const item = root.querySelector('[data-block="1.0.0"]')!;
    const selection = window.getSelection()!;
    const pick = (startNode: Node, startOffset: number, endNode: Node, endOffset: number) => {
      const range = document.createRange();
      range.setStart(startNode, startOffset);
      range.setEnd(endNode, endOffset);
      selection.removeAllRanges();
      selection.addRange(range);
      return selectedPassage(root, body, selection);
    };
    const texts = [...first.childNodes].flatMap((n) => (n.nodeType === Node.TEXT_NODE ? [n] : [...(n as Element).childNodes]));
    const bold = first.querySelector("strong")!.firstChild!;
    const after = texts.at(-1)!;
    expect(pick(first.firstChild!, 2, bold, 2)).toEqual({ path: [0], start: 3, end: 10, quote: "ship on" });
    expect(pick(bold, 0, after, 6)).toEqual({ path: [0], start: 8, end: 29, quote: "on Friday with  after" });
    expect(pick(item.firstChild!, 0, item.firstChild!, 6)).toEqual({ path: [1, 0, 0], start: 0, end: 6, quote: "Friday" });
    expect(pick(after, 1, item, 0)).toEqual({ path: [0], start: 24, end: 37, quote: "after review." });
    expect(pick(first.firstChild!, 0, item.firstChild!, 3)).toBeNull();
    expect(pick(first.firstChild!, 2, first.firstChild!, 3)).toBeNull();
  });

  it("keeps its marks, overlapping ones too, through the page's editor", () => {
    const content = doc(para(text("A "), text("shared", anchor(open), anchor(settled)), text(" passage", anchor(open))));
    const editor = new Editor({ element: document.createElement("div"), extensions: editorExtensions(), content });
    editor.chain().focus("end").insertContent(" more").run();
    const saved = editor.getJSON() as Doc;
    editor.destroy();
    expect(marksIn(saved)).toEqual({ [open]: "shared passage", [settled]: "shared" });
    expect(problems(saved)).toEqual([]);
  });
});

function aComment(threadId: string, words: string, over: Partial<Comment> = {}): Comment {
  return {
    id: threadId,
    threadId,
    authorId: "u-grace",
    authorName: "Grace Hopper",
    body: say(words),
    deleted: false,
    createdAt: "2026-09-29T08:00:00Z",
    editedAt: null,
    can: { edit: false, delete: false },
    ...over,
  };
}

function anInlineThread(id: string, quote: string, words: string, over: Partial<Thread> = {}): Thread {
  return {
    id,
    pageId,
    kind: "inline",
    anchor: { state: "anchored", quote },
    resolved: false,
    resolvedByName: "",
    resolvedAt: null,
    comments: [aComment(id, words)],
    can: { reply: true, resolve: true },
    ...over,
  };
}

const pageBody = doc(
  para(text("We ship "), text("on Friday", anchor(open)), text(" after "), text("the review", anchor(settled)), text(".")),
  para(text("Notes follow.")),
);

const inlineThreads = (): Thread[] => [
  anInlineThread(open, "on Friday", "Why not Monday?"),
  anInlineThread(gone, "the old plan", "This was the old plan.", { anchor: { state: "detached", quote: "the old plan" } }),
  anInlineThread(settled, "the review", "Who reviews?", { resolved: true, resolvedByName: "Ada Lovelace", resolvedAt: "2026-09-29T10:00:00Z" }),
];

function stubInline({
  body = pageBody,
  threads = inlineThreads(),
  more = {},
}: {
  body?: Doc;
  threads?: Thread[];
  more?: Record<string, Answer | ((request: Request) => Answer | Promise<Answer>)>;
} = {}) {
  const page = aPage({ id: pageId, title: "Plan", home: false, body, comments: { page: 0, inline: 1, detached: 1 } });
  const space = aSpace();
  return stubApi({
    "GET /spaces": { status: 200, body: { spaces: [space] } },
    "GET /spaces/DOCS": { status: 200, body: { space } },
    "GET /spaces/DOCS/pages": { status: 200, body: { pages: [] } },
    [`GET /pages/${pageId}`]: { status: 200, body: { page, space } },
    [`GET /pages/${pageId}/attachments`]: { status: 200, body: { attachments: [] } },
    [`GET /pages/${pageId}/comments`]: (request) => ({
      status: 200,
      body: { threads: new URL(request.url).searchParams.get("kind") === "inline" ? threads : [] },
    }),
    ...more,
  });
}

/** Selects words of the page's first paragraph as a reader would, and lets the page notice. */
async function selectWords(words: string) {
  const block = await waitFor(() => {
    const found = document.querySelector<HTMLElement>('[data-passage-root] [data-block="0"]');
    if (!found) throw new Error("no page yet");
    return found;
  });
  const walker = document.createTreeWalker(block, NodeFilter.SHOW_TEXT);
  for (let node = walker.nextNode() as Text | null; node; node = walker.nextNode() as Text | null) {
    const at = node.data.indexOf(words);
    if (at < 0) continue;
    const range = document.createRange();
    range.setStart(node, at);
    range.setEnd(node, at + words.length);
    const selection = window.getSelection()!;
    selection.removeAllRanges();
    selection.addRange(range);
    act(() => {
      document.dispatchEvent(new Event("selectionchange"));
    });
    return;
  }
  throw new Error(`"${words}" is not in one text of the first paragraph`);
}

describe("comments on passages in the reader", () => {
  it("highlights open passages, lists detached ones with their quote, and hides resolved ones until asked", async () => {
    stubInline();
    await renderAt(`/s/DOCS/p/${pageId}/plan`);
    const section = await screen.findByRole("region", { name: /Comments on passages/ });
    await waitFor(() => expect(document.querySelector(`mark[data-passage="${open}"]`)).toHaveTextContent("on Friday"));
    expect(document.querySelector(`mark[data-passage="${settled}"]`)).toBeNull();
    expect(within(section).getByRole("button", { name: "on Friday, 1 comment" })).toBeInTheDocument();
    expect(within(section).getByText("Passages no longer on the page")).toBeInTheDocument();
    const detached = section.querySelector(`[data-thread="${gone}"]`) as HTMLElement;
    expect(within(detached).getByText("the old plan")).toBeInTheDocument();
    expect(within(detached).getByText("This was the old plan.")).toBeInTheDocument();
    expect(section.querySelector(`[data-thread="${settled}"]`)).toBeNull();
    expect(await axeViolations()).toEqual([]);

    const toggle = within(section).getByRole("button", { name: "Show 1 resolved thread" });
    await userEvent.click(toggle);
    expect(toggle).toHaveAttribute("aria-pressed", "true");
    const resolved = section.querySelector(`[data-thread="${settled}"]`) as HTMLElement;
    expect(within(resolved).getByText("Resolved by Ada Lovelace")).toBeInTheDocument();
    expect(within(resolved).getByRole("button", { name: "Reopen" })).toBeInTheDocument();
    expect(document.querySelector(`mark[data-passage="${settled}"]`)).toHaveTextContent("the review");
    expect(await axeViolations()).toEqual([]);
  });

  it("opens a passage's thread in a panel, where it is resolved", async () => {
    const sent = stubInline({
      more: { [`POST /comments/${open}/resolve`]: { status: 200, body: { thread: anInlineThread(open, "on Friday", "x", { resolved: true }) } } },
    });
    await renderAt(`/s/DOCS/p/${pageId}/plan`);
    const passage = await waitFor(() => {
      const found = document.querySelector<HTMLElement>(`mark[data-passage="${open}"]`);
      if (!found) throw new Error("no highlight yet");
      return found;
    });
    await userEvent.click(passage);
    const panel = await screen.findByRole("region", { name: "Comment on a passage" });
    await waitFor(() => expect(panel).toHaveFocus());
    expect(passage).toHaveAttribute("data-active");
    expect(within(panel).getByText("Why not Monday?")).toBeInTheDocument();
    expect(await axeViolations()).toEqual([]);
    await userEvent.click(within(panel).getByRole("button", { name: "Resolve" }));
    await waitFor(() => expect(sent.some((each) => each.method === "POST" && each.path === `/comments/${open}/resolve`)).toBe(true));
    await waitFor(() => expect(screen.queryByRole("region", { name: "Comment on a passage" })).toBeNull());
  });

  it("comments on selected text by the shortcut, marking only that passage in the body it sends", async () => {
    let made = "";
    const sent = stubInline({
      more: {
        [`POST /pages/${pageId}/inline-comments`]: async (request) => {
          const { threadId } = (await request.json()) as { threadId: string };
          made = threadId;
          return { status: 201, body: { thread: anInlineThread(threadId, "Notes", "Which notes?"), page: aPage({ id: pageId, body: pageBody }) } };
        },
      },
    });
    await renderAt(`/s/DOCS/p/${pageId}/plan`);
    await selectWords("We ship");
    const button = await screen.findByRole("button", { name: "Comment" });
    expect(button).toHaveAttribute("aria-keyshortcuts", "Control+Alt+M");
    await userEvent.keyboard("{Control>}{Alt>}m{/Alt}{/Control}");
    const panel = await screen.findByRole("region", { name: "Comment on a passage" });
    expect(within(panel).getByText("We ship")).toBeInTheDocument();
    const box = await waitFor(() => {
      const found = document.getElementById("new-passage-comment");
      if (!found) throw new Error("no editor yet");
      return found;
    });
    await userEvent.click(box);
    await userEvent.type(box, "Which ship?");
    expect(await axeViolations()).toEqual([]);
    await userEvent.click(within(panel).getByRole("button", { name: "Comment" }));
    await waitFor(() => expect(made).not.toBe(""));
    const posted = sent.find((each) => each.method === "POST" && each.path === `/pages/${pageId}/inline-comments`)!.body as { pageBody: Doc; body: Doc };
    expect(marksIn(posted.pageBody)).toEqual({ [open]: "on Friday", [settled]: "the review", [made]: "We ship" });
    expect(JSON.stringify(posted.body)).toContain("Which ship?");
  });

  it("marks the passage afresh when the page changed meanwhile", async () => {
    const changed = doc(para(text("Now "), ...pageBody.content![0]!.content!), para(text("Notes follow.")));
    let calls = 0;
    const bodies: Doc[] = [];
    stubInline({
      more: {
        [`POST /pages/${pageId}/inline-comments`]: async (request) => {
          const { threadId, pageBody: sentBody } = (await request.json()) as { threadId: string; pageBody: Doc };
          bodies.push(sentBody);
          calls += 1;
          if (calls === 1) {
            stubInline({
              body: changed,
              more: {
                [`POST /pages/${pageId}/inline-comments`]: async (again) => {
                  bodies.push(((await again.json()) as { pageBody: Doc }).pageBody);
                  return { status: 201, body: { thread: anInlineThread(threadId, "after", "Why?"), page: aPage({ id: pageId, body: changed }) } };
                },
              },
            });
            return {
              status: 409,
              body: {
                error: {
                  code: "anchor_conflict",
                  message: "The page changed after you selected the passage. Read the page again and select the passage once more.",
                },
              },
            };
          }
          return { status: 500 };
        },
      },
    });
    await renderAt(`/s/DOCS/p/${pageId}/plan`);
    await selectWords("after");
    await userEvent.click(await screen.findByRole("button", { name: "Comment" }));
    const box = await waitFor(() => {
      const found = document.getElementById("new-passage-comment");
      if (!found) throw new Error("no editor yet");
      return found;
    });
    await userEvent.click(box);
    await userEvent.type(box, "Why?");
    await userEvent.click(within(await screen.findByRole("region", { name: "Comment on a passage" })).getByRole("button", { name: "Comment" }));
    await waitFor(() => expect(bodies).toHaveLength(2));
    const fresh = Object.entries(marksIn(bodies[1]!)).find(([id]) => id !== open && id !== settled)!;
    expect(fresh[1]).toBe("after");
    expect(bodies[1]!.content![0]!.content![0]).toEqual(text("Now "));
  });
});
