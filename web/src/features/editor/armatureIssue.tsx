import { InputRule, Node, mergeAttributes } from "@tiptap/core";
import type { EditorState } from "@tiptap/pm/state";
import { Plugin, PluginKey } from "@tiptap/pm/state";
import { NodeViewWrapper, ReactNodeViewRenderer, type NodeViewProps } from "@tiptap/react";
import { IssueChip } from "@/features/armature/IssueChip";
import { ARMATURE_ISSUE_NODE, TYPED_KEY, keyFromIssueUrl, normalizeKey, projectOf } from "@/features/armature/issueKeys";

/** What the editor needs to know to turn text into chips, read when it happens. */
export interface IssueSource {
  /** Where the organization's Armature opens, or null when none is connected. */
  baseUrl: () => string | null;
  /** Whether the author may see a project in Armature, so a typed key in it becomes a chip. */
  knowsProject: (projectKey: string) => boolean;
}

export interface ArmatureIssueOptions {
  source: IssueSource | undefined;
}

// Nothing converts in code: a key there is the code's own text.
function inCode(state: EditorState, pos: number): boolean {
  const $pos = state.doc.resolve(pos);
  return Boolean($pos.parent.type.spec.code) || $pos.marks().some((mark) => mark.type.spec.code);
}

function ChipView({ node }: NodeViewProps) {
  return (
    <NodeViewWrapper as="span">
      <IssueChip issueKey={String(node.attrs.key ?? "")} links={false} />
    </NodeViewWrapper>
  );
}

/**
 * An Armature issue in running text: its key and nothing else, so nothing about
 * the issue is stored in the page. Each reader's view looks the issue up as them.
 */
export const ArmatureIssue = Node.create<ArmatureIssueOptions>({
  name: ARMATURE_ISSUE_NODE,
  group: "inline",
  inline: true,
  atom: true,
  selectable: true,
  // Before the link extension, which would otherwise make a pasted issue
  // address a plain link.
  priority: 1100,
  addOptions() {
    return { source: undefined };
  },
  addAttributes() {
    return {
      key: {
        default: null,
        parseHTML: (el) => normalizeKey(el.getAttribute("data-armature-issue")),
        renderHTML: (attrs) => ({ "data-armature-issue": attrs.key }),
      },
    };
  },
  parseHTML() {
    return [{ tag: "span[data-armature-issue]", getAttrs: (el) => (normalizeKey((el as HTMLElement).getAttribute("data-armature-issue")) ? null : false) }];
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
  addInputRules() {
    const source = this.options.source;
    const type = this.type;
    return [
      new InputRule({
        find: TYPED_KEY,
        handler: ({ state, range, match }) => {
          const key = match[1];
          const after = match[2] ?? "";
          if (!key || !source?.knowsProject(projectOf(key))) return null;
          const from = range.from + match[0].length - after.length - key.length;
          if (inCode(state, from)) return null;
          const parts = after ? [type.create({ key }), state.schema.text(after)] : [type.create({ key })];
          state.tr.replaceWith(from, range.to, parts);
        },
      }),
    ];
  },
  addProseMirrorPlugins() {
    const source = this.options.source;
    const type = this.type;
    return [
      new Plugin({
        key: new PluginKey("armatureIssuePaste"),
        props: {
          handlePaste: (view, event) => {
            const text = event.clipboardData?.getData("text/plain") ?? "";
            const key = keyFromIssueUrl(text, source?.baseUrl());
            if (!key || inCode(view.state, view.state.selection.from)) return false;
            view.dispatch(view.state.tr.replaceSelectionWith(type.create({ key }), false).scrollIntoView());
            return true;
          },
        },
      }),
    ];
  },
});
