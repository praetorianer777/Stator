import { useEffect, useRef, useState } from "react";
import { Node, mergeAttributes } from "@tiptap/core";
import type { Node as PMNode } from "@tiptap/pm/model";
import { Plugin, PluginKey, type EditorState, type Transaction } from "@tiptap/pm/state";
import { NodeViewContent, NodeViewWrapper, ReactNodeViewRenderer, type NodeViewProps } from "@tiptap/react";
import { EXCERPT_NAME_MAX_LENGTH } from "@/config";
import { t } from "@/i18n";

declare module "@tiptap/core" {
  interface Commands<ReturnType> {
    excerpt: {
      /** Marks the selected blocks as an excerpt with a name of its own, and puts the caret in its name. */
      setExcerpt: () => ReturnType;
      /** Takes the excerpt away and keeps what was inside it. */
      unsetExcerpt: () => ReturnType;
    };
  }
  interface Storage {
    excerpt: { focusName: boolean };
  }
}

export const EXCERPT_NODE = "excerpt";

const UUID = /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/;

/** A name the API takes: some words, cut at its limit; null when there are none. */
export function excerptName(value: unknown): string | null {
  if (typeof value !== "string" || !value.trim()) return null;
  return [...value].slice(0, EXCERPT_NAME_MAX_LENGTH).join("");
}

const nameKey = (name: string) => name.trim().toLowerCase();

/** The first stock name no excerpt has. */
export function freshName(taken: Set<string>): string {
  for (let n = 1; ; n++) {
    const name = t.editor.excerpt.defaultName(n);
    if (!taken.has(nameKey(name))) return name;
  }
}

function excerptsOf(doc: PMNode): Array<{ node: PMNode; pos: number; nested: boolean }> {
  const out: Array<{ node: PMNode; pos: number; nested: boolean }> = [];
  doc.descendants((node, pos, parent) => {
    if (node.type.name !== EXCERPT_NODE) return true;
    let nested = false;
    const $pos = doc.resolve(pos);
    for (let d = $pos.depth; d > 0; d--) if ($pos.node(d).type.name === EXCERPT_NODE) nested = true;
    out.push({ node, pos, nested: nested || parent?.type.name === EXCERPT_NODE });
    return true;
  });
  return out;
}

/**
 * Keeps a page's excerpts as the server takes them: an excerpt pasted into
 * another gives up its frame and keeps its blocks, a copy gets an id of its
 * own, and a second of a name gets a stock one. The id an excerpt had stays,
 * so a page that includes it keeps finding it.
 */
export function tidyExcerpts(state: EditorState): Transaction | null {
  const found = excerptsOf(state.doc);
  if (found.length === 0) return null;
  let tr: Transaction | null = null;
  const nested = found.filter((e) => e.nested);
  // From the end, so lifting one does not move those before it.
  for (const e of nested.reverse()) {
    tr ??= state.tr;
    const pos = tr.mapping.map(e.pos);
    const node = tr.doc.nodeAt(pos);
    if (node?.type.name === EXCERPT_NODE) tr.replaceWith(pos, pos + node.nodeSize, node.content);
  }
  if (tr) return tr.setMeta("addToHistory", false);
  const ids = new Set<string>();
  const names = new Set(found.map((e) => nameKey(String(e.node.attrs.name ?? ""))).filter(Boolean));
  const seenNames = new Set<string>();
  for (const e of found) {
    const attrs = { ...e.node.attrs };
    let changed = false;
    if (typeof attrs.id !== "string" || !UUID.test(attrs.id) || ids.has(attrs.id)) {
      attrs.id = crypto.randomUUID();
      changed = true;
    }
    ids.add(attrs.id);
    const name = excerptName(attrs.name);
    if (!name || seenNames.has(nameKey(name))) {
      attrs.name = freshName(names);
      names.add(nameKey(attrs.name));
      changed = true;
    }
    seenNames.add(nameKey(String(attrs.name)));
    if (changed) {
      tr ??= state.tr;
      tr.setNodeMarkup(e.pos, undefined, attrs);
    }
  }
  return tr ? (tr as Transaction).setMeta("addToHistory", false) : null;
}

// The name box is the block's own: ProseMirror would otherwise take its keys
// and clicks as editing the document around it.
const ownEvent = ({ event }: { event: Event }) => event.target instanceof Element && event.target.closest("[data-excerpt-name]") !== null;

function ExcerptNodeView({ node, editor, updateAttributes }: NodeViewProps) {
  const input = useRef<HTMLInputElement>(null);
  const stored = String(node.attrs.name ?? "");
  // The box may be emptied while a name is typed; the page keeps the last
  // name until there is a new one, since an excerpt always has one.
  const [typed, setTyped] = useState(stored);
  useEffect(() => setTyped(stored), [stored]);
  useEffect(() => {
    const storage = editor.storage.excerpt;
    if (!storage.focusName) return;
    const frame = requestAnimationFrame(() => {
      storage.focusName = false;
      input.current?.focus();
      input.current?.select();
    });
    return () => cancelAnimationFrame(frame);
  }, [editor]);
  return (
    <NodeViewWrapper className="doc-excerpt doc-excerpt-edit" data-excerpt={String(node.attrs.id ?? "")}>
      <div className="doc-excerpt-head" contentEditable={false}>
        <label className="doc-excerpt-label">
          {t.editor.excerpt.label}
          <input
            ref={input}
            type="text"
            className="doc-excerpt-name"
            maxLength={EXCERPT_NAME_MAX_LENGTH}
            value={typed}
            readOnly={!editor.isEditable}
            onChange={(event) => {
              setTyped(event.target.value);
              const name = excerptName(event.target.value);
              if (name) updateAttributes({ name });
            }}
            onBlur={() => setTyped(stored)}
            data-excerpt-name=""
          />
        </label>
      </div>
      <NodeViewContent className="doc-excerpt-body" data-excerpt-body="" />
    </NodeViewWrapper>
  );
}

/** A named part of a page that other pages include, kept by an id that outlives a rename. */
export const Excerpt = Node.create<Record<string, never>, { focusName: boolean }>({
  name: EXCERPT_NODE,
  group: "block",
  content: "block+",
  defining: true,
  addStorage() {
    return { focusName: false };
  },
  addAttributes() {
    return {
      id: {
        default: null,
        parseHTML: (el) => {
          const id = el.getAttribute("data-excerpt");
          return id && UUID.test(id) ? id : null;
        },
        renderHTML: (attrs) => ({ "data-excerpt": attrs.id }),
      },
      name: {
        default: "",
        parseHTML: (el) => excerptName(el.getAttribute("data-excerpt-title")) ?? "",
        renderHTML: (attrs) => ({ "data-excerpt-title": attrs.name }),
      },
    };
  },
  parseHTML() {
    return [{ tag: "div[data-excerpt]", contentElement: (el: HTMLElement) => el.querySelector<HTMLElement>(":scope > [data-excerpt-body]") ?? el }];
  },
  renderHTML({ HTMLAttributes }) {
    return ["div", mergeAttributes(HTMLAttributes), ["div", { "data-excerpt-body": "" }, 0]];
  },
  addNodeView() {
    return ReactNodeViewRenderer(ExcerptNodeView, { stopEvent: ownEvent });
  },
  addCommands() {
    return {
      setExcerpt:
        () =>
        ({ state, commands, dispatch, editor }) => {
          const { $from } = state.selection;
          for (let d = $from.depth; d > 0; d--) if ($from.node(d).type.name === this.name) return false;
          const taken = new Set(excerptsOf(state.doc).map((e) => nameKey(String(e.node.attrs.name ?? ""))));
          const done = commands.wrapIn(this.name, { id: crypto.randomUUID(), name: freshName(taken) });
          if (done && dispatch) editor.storage.excerpt.focusName = true;
          return done;
        },
      unsetExcerpt:
        () =>
        ({ commands }) =>
          commands.lift(this.name),
    };
  },
  addProseMirrorPlugins() {
    return [
      new Plugin({
        key: new PluginKey("excerptTidy"),
        appendTransaction: (transactions, _old, state) => (transactions.some((tr) => tr.docChanged) ? tidyExcerpts(state) : null),
      }),
    ];
  },
});
