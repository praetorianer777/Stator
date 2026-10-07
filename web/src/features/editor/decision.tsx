import { Node, mergeAttributes } from "@tiptap/core";
import { NodeViewContent, NodeViewWrapper, ReactNodeViewRenderer, type NodeViewProps } from "@tiptap/react";
import { t } from "@/i18n";

export const DECISION_STATES = ["decided", "undecided"] as const;
export type DecisionState = (typeof DECISION_STATES)[number];

declare module "@tiptap/core" {
  interface Commands<ReturnType> {
    decision: {
      /** Turns the line under the caret into a decision item, not yet decided. */
      setDecision: () => ReturnType;
    };
  }
}

/** A stored state, or undecided for anything the server would refuse. */
export function decisionState(value: unknown): DecisionState {
  return value === "decided" ? "decided" : "undecided";
}

// The state button is the item's own: ProseMirror would otherwise take its
// clicks as placing the caret.
const ownEvent = ({ event }: { event: Event }) => event.target instanceof Element && event.target.closest("[data-decision-toggle]") !== null;

function DecisionNodeView({ node, editor, updateAttributes }: NodeViewProps) {
  const state = decisionState(node.attrs.state);
  const decided = state === "decided";
  return (
    <NodeViewWrapper className="doc-decision" data-decision={state}>
      <button
        type="button"
        contentEditable={false}
        className="doc-decision-badge"
        aria-pressed={decided}
        disabled={!editor.isEditable}
        onClick={() => updateAttributes({ state: decided ? "undecided" : "decided" })}
        data-decision-toggle=""
      >
        {decided ? t.editor.decision.decided : t.editor.decision.undecided}
      </button>
      <NodeViewContent className="doc-decision-text" data-decision-text="" />
    </NodeViewWrapper>
  );
}

/** A decision item: one line and whether it is decided, which a space's decision log quotes. */
export const Decision = Node.create({
  name: "decision",
  group: "block",
  content: "inline*",
  defining: true,
  addAttributes() {
    return {
      state: {
        default: "undecided",
        parseHTML: (el) => decisionState(el.getAttribute("data-decision")),
        renderHTML: (attrs) => ({ "data-decision": decisionState(attrs.state) }),
      },
    };
  },
  parseHTML() {
    return [{ tag: "div[data-decision]", contentElement: (el: HTMLElement) => el.querySelector<HTMLElement>("[data-decision-text]") ?? el }];
  },
  renderHTML({ HTMLAttributes }) {
    return ["div", mergeAttributes(HTMLAttributes, { class: "doc-decision" }), ["p", { "data-decision-text": "" }, 0]];
  },
  addNodeView() {
    return ReactNodeViewRenderer(DecisionNodeView, { stopEvent: ownEvent });
  },
  addCommands() {
    return {
      setDecision:
        () =>
        ({ commands }) =>
          commands.setNode(this.name, { state: "undecided" }),
    };
  },
});
