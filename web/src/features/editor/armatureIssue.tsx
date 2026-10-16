import { Extension, InputRule, Node, mergeAttributes } from "@tiptap/core";
import type { EditorState } from "@tiptap/pm/state";
import { Plugin, PluginKey } from "@tiptap/pm/state";
import { NodeViewWrapper, ReactNodeViewRenderer, type NodeViewProps } from "@tiptap/react";
import Suggestion, { type SuggestionOptions } from "@tiptap/suggestion";
import type { ArmatureIssue as FoundIssue } from "@/api/armature";
import { ARMATURE_FIND_CLOSE, ARMATURE_FIND_TRIGGER } from "@/config";
import { IssueChip } from "@/features/armature/IssueChip";
import { ARMATURE_ISSUE_NODE, TYPED_KEY, keyFromIssueUrl, normalizeKey, projectOf } from "@/features/armature/issueKeys";

/** What the editor needs to know to turn text into chips, read when it happens. */
export interface IssueSource {
  /** Where the organization's Armature opens, or null when none is connected. */
  baseUrl: () => string | null;
  /** Whether the author may see a project in Armature, so a typed key in it becomes a chip. */
  knowsProject: (projectKey: string) => boolean;
  /** Whether the author may turn a selection into issues: a connection and their own token. */
  canCreate?: () => boolean;
  /** The page whose selection becomes issues, which their descriptions link back to. */
  pageId?: () => string | null;
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
          // Typed after the trigger, a key is what the menu looks for, not a chip yet.
          if (issueFindKey.getState(state)?.active) return null;
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

/** What the inline menu hands its command: the picked issue's key. */
export interface FoundKey {
  key: string;
}

export interface IssueFindOptions {
  source: IssueSource | undefined;
  suggestion: Omit<SuggestionOptions<FoundIssue, FoundKey>, "editor">;
}

export const issueFindKey = new PluginKey("armatureIssueFind");

/**
 * Two opening brackets start a menu at the caret that finds an Armature
 * issue by words or key; the one picked goes in as a chip. Escape, or the
 * closing brackets, leave the typed text as it is.
 */
export const ArmatureIssueFind = Extension.create<IssueFindOptions>({
  name: "armatureIssueFind",
  addOptions() {
    return {
      source: undefined,
      suggestion: {
        char: ARMATURE_FIND_TRIGGER,
        pluginKey: issueFindKey,
        allowSpaces: true,
        command: ({ editor, range, props }) => {
          editor
            .chain()
            .focus()
            .insertContentAt(range, [
              { type: ARMATURE_ISSUE_NODE, attrs: { key: props.key } },
              { type: "text", text: " " },
            ])
            .run();
        },
      },
    };
  },
  addProseMirrorPlugins() {
    const source = this.options.source;
    return [
      Suggestion({
        editor: this.editor,
        ...this.options.suggestion,
        allow: ({ state, range }) =>
          Boolean(source?.baseUrl()) && !inCode(state, range.from) && !state.doc.textBetween(range.from, range.to).includes(ARMATURE_FIND_CLOSE),
      }),
    ];
  },
});
