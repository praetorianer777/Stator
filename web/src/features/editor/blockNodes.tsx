import { useId, type MouseEvent } from "react";
import { Node, mergeAttributes } from "@tiptap/core";
import { NodeViewWrapper, ReactNodeViewRenderer, useEditorState, type NodeViewProps } from "@tiptap/react";
import { SelectInput } from "@/components/ui";
import { TOC_DEFAULT_MAX_LEVEL } from "@/config";
import { t } from "@/i18n";
import { CHILD_PAGES_DEPTH_CHOICES, ChildPagesList, TOC_LEVEL_CHOICES, TocList } from "./BlockViews";
import { childPagesOptions, defaultChildPages, type ChildPagesOptions } from "./childPages";
import { CHILD_PAGES_SCOPES, CHILD_PAGES_SORTS } from "./schema";
import { buildToc, headingsOfEditor, tocMaxLevel } from "./toc";

declare module "@tiptap/core" {
  interface Commands<ReturnType> {
    pageBlocks: {
      insertTableOfContents: () => ReturnType;
      insertChildPages: () => ReturnType;
    };
  }
}

// Links and the settings are the block's own: ProseMirror would otherwise
// take a click on them as selecting the block.
const ownEvent = ({ event }: { event: Event }) => event.target instanceof Element && event.target.closest("a, select, button, label") !== null;

function TocNodeView({ node, editor, updateAttributes }: NodeViewProps) {
  const id = useId();
  const maxLevel = tocMaxLevel(node.attrs.maxLevel);
  const headings = useEditorState({ editor, selector: ({ editor: e }) => headingsOfEditor(e.state.doc) });
  const follow = (anchor: string, event: MouseEvent<HTMLAnchorElement>) => {
    event.preventDefault();
    const target = editor.view.dom.querySelector(`[data-anchor="${CSS.escape(anchor)}"]`);
    target?.scrollIntoView?.({ block: "start" });
  };
  return (
    <NodeViewWrapper as="nav" aria-label={t.editor.toc.label} data-toc className="doc-block" contentEditable={false}>
      <div className="doc-block-settings" data-block-settings>
        <label htmlFor={`${id}-levels`}>{t.editor.toc.levels}</label>
        <SelectInput id={`${id}-levels`} controlSize="sm" value={maxLevel} onChange={(event) => updateAttributes({ maxLevel: Number(event.target.value) })}>
          {TOC_LEVEL_CHOICES.map((level) => (
            <option key={level} value={level}>
              {t.editor.toc.levelOption(level)}
            </option>
          ))}
        </SelectInput>
      </div>
      <TocList entries={buildToc(headings ?? [], maxLevel)} onFollow={follow} />
    </NodeViewWrapper>
  );
}

function ChildPagesNodeView({ node, updateAttributes }: NodeViewProps) {
  const id = useId();
  const options = childPagesOptions(node.attrs);
  const set = (change: Partial<ChildPagesOptions>) => updateAttributes({ ...options, ...change });
  const c = t.editor.childPages;
  return (
    <NodeViewWrapper as="nav" aria-label={c.label} data-child-pages className="doc-block" contentEditable={false}>
      <div className="doc-block-settings" data-block-settings>
        <label htmlFor={`${id}-scope`}>{c.scope}</label>
        <SelectInput
          id={`${id}-scope`}
          controlSize="sm"
          value={options.scope}
          onChange={(event) => set({ scope: event.target.value as ChildPagesOptions["scope"] })}
        >
          {CHILD_PAGES_SCOPES.map((scope) => (
            <option key={scope} value={scope}>
              {c.scopes[scope]}
            </option>
          ))}
        </SelectInput>
        {options.scope === "subtree" && (
          <>
            <label htmlFor={`${id}-depth`}>{c.depth}</label>
            <SelectInput
              id={`${id}-depth`}
              controlSize="sm"
              value={options.depth ?? ""}
              onChange={(event) => set({ depth: event.target.value ? Number(event.target.value) : null })}
            >
              <option value="">{c.everyLevel}</option>
              {CHILD_PAGES_DEPTH_CHOICES.map((depth) => (
                <option key={depth} value={depth}>
                  {c.levelCount(depth)}
                </option>
              ))}
            </SelectInput>
          </>
        )}
        <label htmlFor={`${id}-sort`}>{c.sort}</label>
        <SelectInput
          id={`${id}-sort`}
          controlSize="sm"
          value={options.sort}
          onChange={(event) => set({ sort: event.target.value as ChildPagesOptions["sort"] })}
        >
          {CHILD_PAGES_SORTS.map((sort) => (
            <option key={sort} value={sort}>
              {c.sorts[sort]}
            </option>
          ))}
        </SelectInput>
      </div>
      <ChildPagesList options={options} />
    </NodeViewWrapper>
  );
}

/** A table of contents of the page's headings, drawn afresh from them as they change. */
export const TableOfContents = Node.create({
  name: "tableOfContents",
  group: "block",
  atom: true,
  selectable: true,
  draggable: true,
  addAttributes() {
    return {
      maxLevel: {
        default: TOC_DEFAULT_MAX_LEVEL,
        parseHTML: (el) => tocMaxLevel(Number(el.getAttribute("data-max-level"))),
        renderHTML: (attrs) => ({ "data-max-level": attrs.maxLevel }),
      },
    };
  },
  parseHTML() {
    return [{ tag: "nav[data-toc]" }];
  },
  renderHTML({ HTMLAttributes }) {
    return ["nav", mergeAttributes(HTMLAttributes, { "data-toc": "" })];
  },
  addNodeView() {
    return ReactNodeViewRenderer(TocNodeView, { stopEvent: ownEvent });
  },
  addCommands() {
    return {
      insertTableOfContents:
        () =>
        ({ commands }) =>
          commands.insertContent({ type: this.name, attrs: { maxLevel: TOC_DEFAULT_MAX_LEVEL } }),
    };
  },
});

/** A list of the pages below the page, which the reader's view fetches as its reader may see them. */
export const ChildPages = Node.create({
  name: "childPages",
  group: "block",
  atom: true,
  selectable: true,
  draggable: true,
  addAttributes() {
    const read = (el: HTMLElement) =>
      childPagesOptions({ scope: el.getAttribute("data-scope"), depth: Number(el.getAttribute("data-depth")) || null, sort: el.getAttribute("data-sort") });
    return {
      scope: { default: defaultChildPages.scope, parseHTML: (el) => read(el).scope, renderHTML: (attrs) => ({ "data-scope": attrs.scope }) },
      depth: { default: defaultChildPages.depth, parseHTML: (el) => read(el).depth, renderHTML: (attrs) => (attrs.depth ? { "data-depth": attrs.depth } : {}) },
      sort: { default: defaultChildPages.sort, parseHTML: (el) => read(el).sort, renderHTML: (attrs) => ({ "data-sort": attrs.sort }) },
    };
  },
  parseHTML() {
    return [{ tag: "nav[data-child-pages]" }];
  },
  renderHTML({ HTMLAttributes }) {
    return ["nav", mergeAttributes(HTMLAttributes, { "data-child-pages": "" })];
  },
  addNodeView() {
    return ReactNodeViewRenderer(ChildPagesNodeView, { stopEvent: ownEvent });
  },
  addCommands() {
    return {
      insertChildPages:
        () =>
        ({ commands }) =>
          commands.insertContent({ type: this.name, attrs: { ...defaultChildPages } }),
    };
  },
});
