import { InputRule, Node, mergeAttributes } from "@tiptap/core";
import type { EditorState } from "@tiptap/pm/state";
import { NodeViewWrapper, ReactNodeViewRenderer, type NodeViewProps } from "@tiptap/react";
import { RequirementChip } from "@/features/exciter/RequirementChip";
import { EXCITER_REQUIREMENT_NODE, TYPED_REQUIREMENT_KEY, normalizeRequirementKey } from "@/features/exciter/requirementKeys";

declare module "@tiptap/core" {
  interface Commands<ReturnType> {
    exciterRequirement: {
      /** Opens the dialog that asks which requirement; the dialog inserts it. */
      pickExciterRequirement: () => ReturnType;
      insertExciterRequirement: (key: string) => ReturnType;
    };
  }
}

/** What the editor needs to know to turn a typed key into a chip, read when it happens. */
export interface RequirementSource {
  /** Where Exciter opens, or null when Stator is not connected to one. */
  baseUrl: () => string | null;
  /** Whether the author connected their own Exciter account, so a typed key becomes a chip. */
  connected: () => boolean;
}

export interface ExciterRequirementOptions {
  source: RequirementSource | undefined;
  /** Opens the key dialog; without it the slash menu's entry does nothing. */
  pick: (() => void) | undefined;
}

// Nothing converts in code: a key there is the code's own text.
function inCode(state: EditorState, pos: number): boolean {
  const $pos = state.doc.resolve(pos);
  return Boolean($pos.parent.type.spec.code) || $pos.marks().some((mark) => mark.type.spec.code);
}

function ChipView({ node }: NodeViewProps) {
  return (
    <NodeViewWrapper as="span">
      <RequirementChip requirementKey={String(node.attrs.key ?? "")} links={false} />
    </NodeViewWrapper>
  );
}

/**
 * An Exciter requirement in running text: its key and nothing else, so nothing
 * about the requirement is stored in the page. Each reader's view reads it as them.
 */
export const ExciterRequirement = Node.create<ExciterRequirementOptions>({
  name: EXCITER_REQUIREMENT_NODE,
  group: "inline",
  inline: true,
  atom: true,
  selectable: true,
  addOptions() {
    return { source: undefined, pick: undefined };
  },
  addAttributes() {
    return {
      key: {
        default: null,
        parseHTML: (el) => normalizeRequirementKey(el.getAttribute("data-exciter-requirement")),
        renderHTML: (attrs) => ({ "data-exciter-requirement": attrs.key }),
      },
    };
  },
  parseHTML() {
    return [
      {
        tag: "span[data-exciter-requirement]",
        getAttrs: (el) => (normalizeRequirementKey((el as HTMLElement).getAttribute("data-exciter-requirement")) ? null : false),
      },
    ];
  },
  renderHTML({ node, HTMLAttributes }) {
    return ["span", mergeAttributes(HTMLAttributes), String(node.attrs.key ?? "")];
  },
  renderText({ node }) {
    return String(node.attrs.key ?? "");
  },
  addNodeView() {
    return ReactNodeViewRenderer(ChipView, { as: "span" });
  },
  addCommands() {
    return {
      pickExciterRequirement: () => () => {
        this.options.pick?.();
        return true;
      },
      insertExciterRequirement:
        (key) =>
        ({ commands }) => {
          const normalized = normalizeRequirementKey(key);
          return normalized
            ? commands.insertContent([
                { type: this.name, attrs: { key: normalized } },
                { type: "text", text: " " },
              ])
            : false;
        },
    };
  },
  addInputRules() {
    const source = this.options.source;
    const type = this.type;
    return [
      new InputRule({
        find: TYPED_REQUIREMENT_KEY,
        handler: ({ state, range, match }) => {
          const key = match[1];
          const after = match[2] ?? "";
          // Only for an author Exciter answers, so a key-like word on a page
          // without Exciter stays the text it is.
          if (!key || !normalizeRequirementKey(key) || !source?.baseUrl() || !source.connected()) return null;
          const from = range.from + match[0].length - after.length - key.length;
          if (inCode(state, from)) return null;
          const parts = after ? [type.create({ key }), state.schema.text(after)] : [type.create({ key })];
          state.tr.replaceWith(from, range.to, parts);
        },
      }),
    ];
  },
});
