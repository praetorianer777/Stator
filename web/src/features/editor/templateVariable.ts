import { Node, mergeAttributes } from "@tiptap/core";
import { t } from "@/i18n";
import { TEMPLATE_VARIABLE_NODE, blankText, variableName } from "./blanks";

declare module "@tiptap/core" {
  interface Commands<ReturnType> {
    templateVariable: {
      /** Puts a template's blank for the variable named where the caret is. */
      insertVariable: (name: string) => ReturnType;
    };
  }
}

/** A template's blank, where a value the author gives goes; only a template's editor has it. */
export const TemplateVariable = Node.create({
  name: TEMPLATE_VARIABLE_NODE,
  group: "inline",
  inline: true,
  atom: true,
  selectable: true,
  addAttributes() {
    return {
      name: {
        default: null,
        parseHTML: (el) => variableName(el.getAttribute("data-template-variable")),
        renderHTML: (attrs) => ({ "data-template-variable": attrs.name }),
      },
    };
  },
  parseHTML() {
    return [
      { tag: "span[data-template-variable]", getAttrs: (el) => (variableName((el as HTMLElement).getAttribute("data-template-variable")) ? null : false) },
    ];
  },
  renderHTML({ node, HTMLAttributes }) {
    const name = String(node.attrs.name ?? "");
    return ["span", mergeAttributes(HTMLAttributes, { class: "doc-variable", "aria-label": t.templates.blankLabel(name) }), blankText(name)];
  },
  renderText({ node }) {
    return blankText(String(node.attrs.name ?? ""));
  },
  addCommands() {
    return {
      insertVariable:
        (name) =>
        ({ commands }) =>
          variableName(name) !== null && commands.insertContent({ type: this.name, attrs: { name } }),
    };
  },
});
