import { Node, mergeAttributes } from "@tiptap/core";
import { TextSelection } from "@tiptap/pm/state";
import type { Node as PMNode } from "@tiptap/pm/model";
import type { EditorView } from "@tiptap/pm/view";
import { PROPERTIES_MAX_ROWS, PROPERTY_KEY_MAX_LENGTH } from "@/config";
import { t } from "@/i18n";

export const PROPERTIES_NODE = "properties";
export const PROPERTY_ROW_NODE = "propertyRow";

declare module "@tiptap/core" {
  interface Commands<ReturnType> {
    properties: {
      /** Puts a properties table below the caret with the rows a register usually starts with. */
      insertProperties: () => ReturnType;
    };
  }
}

/** A stored name as the server takes it, cut at its limit. */
export function propertyKey(value: unknown): string {
  return typeof value === "string" ? [...value].slice(0, PROPERTY_KEY_MAX_LENGTH).join("") : "";
}

/** Focuses the name field of the row at pos; the view draws a dispatch at once, so it is there. */
function focusName(view: EditorView, pos: number) {
  const dom = view.nodeDOM(pos);
  if (dom instanceof HTMLElement) dom.querySelector<HTMLInputElement>("input[data-property-key]")?.focus();
}

/**
 * One property: its name in a field of its own, its value as words that take
 * marks, mentions, dates and statuses, as a page's metadata usually does.
 */
export const PropertyRow = Node.create({
  name: PROPERTY_ROW_NODE,
  content: "inline*",
  defining: true,
  isolating: true,
  addAttributes() {
    return {
      key: {
        default: "",
        parseHTML: (el) => propertyKey(el.getAttribute("data-key")),
        renderHTML: (attrs) => ({ "data-key": propertyKey(attrs.key) }),
      },
    };
  },
  parseHTML() {
    return [{ tag: "tr[data-property-row]", contentElement: "td" }];
  },
  renderHTML({ node, HTMLAttributes }) {
    return ["tr", mergeAttributes(HTMLAttributes, { "data-property-row": "" }), ["th", { scope: "row" }, propertyKey(node.attrs.key)], ["td", 0]];
  },
  addNodeView() {
    return ({ node, getPos, editor }) => {
      let current: PMNode = node;
      const row = document.createElement("tr");
      row.setAttribute("data-property-row", "");
      const head = document.createElement("th");
      head.scope = "row";
      head.contentEditable = "false";
      const input = document.createElement("input");
      input.type = "text";
      input.maxLength = PROPERTY_KEY_MAX_LENGTH;
      input.placeholder = t.editor.properties.keyPlaceholder;
      input.setAttribute("aria-label", t.editor.properties.keyLabel);
      input.setAttribute("data-property-key", "");
      input.className = "doc-property-key";
      input.value = propertyKey(node.attrs.key);
      input.disabled = !editor.isEditable;
      const value = document.createElement("td");
      value.className = "doc-property-value";
      head.append(input);
      row.append(head, value);

      input.addEventListener("input", () => {
        const pos = getPos();
        if (typeof pos !== "number") return;
        editor.view.dispatch(editor.view.state.tr.setNodeAttribute(pos, "key", propertyKey(input.value)));
      });
      // Enter and the arrow down go on to the value, as in a form.
      input.addEventListener("keydown", (event) => {
        if (event.key !== "Enter" && event.key !== "ArrowDown") return;
        event.preventDefault();
        const pos = getPos();
        if (typeof pos !== "number") return;
        const at = pos + 1 + current.content.size;
        editor.view.dispatch(editor.view.state.tr.setSelection(TextSelection.create(editor.view.state.doc, at)));
        editor.view.focus();
      });
      return {
        dom: row,
        contentDOM: value,
        update(next) {
          if (next.type.name !== PROPERTY_ROW_NODE) return false;
          current = next;
          const key = propertyKey(next.attrs.key);
          // The field being typed in already shows what it holds.
          if (input.value !== key && document.activeElement !== input) input.value = key;
          return true;
        },
        stopEvent: (event) => event.target === input,
        ignoreMutation: (mutation) => head.contains(mutation.target),
      };
    };
  },
  addKeyboardShortcuts() {
    const inRow = () => this.editor.state.selection.$from.parent.type.name === this.name;
    return {
      // Enter ends a value and starts the next property, its name first.
      Enter: () => {
        if (!inRow()) return false;
        const { $from } = this.editor.state.selection;
        const block = $from.node(-1);
        if (block.childCount >= PROPERTIES_MAX_ROWS) return true;
        const after = $from.after();
        const view = this.editor.view;
        view.dispatch(view.state.tr.insert(after, this.type.create({ key: "" })));
        focusName(view, after);
        return true;
      },
      // Backspace in an empty value takes the row out, a name it had too,
      // unless it is the table's last.
      Backspace: () => {
        if (!inRow()) return false;
        const { $from, empty } = this.editor.state.selection;
        if (!empty || $from.parentOffset > 0 || $from.parent.content.size > 0 || $from.node(-1).childCount < 2) return false;
        const view = this.editor.view;
        const before = $from.before();
        const tr = view.state.tr.delete(before, $from.after());
        view.dispatch(tr.setSelection(TextSelection.near(tr.doc.resolve(Math.max(0, before - 1)), -1)));
        return true;
      },
    };
  },
});

/** A page's metadata as a table of names and values, which a properties report gathers. */
export const Properties = Node.create({
  name: PROPERTIES_NODE,
  group: "block",
  content: `${PROPERTY_ROW_NODE}+`,
  isolating: true,
  parseHTML() {
    return [{ tag: "table[data-properties]", contentElement: "tbody" }];
  },
  renderHTML({ HTMLAttributes }) {
    return ["table", mergeAttributes(HTMLAttributes, { "data-properties": "", class: "doc-properties" }), ["tbody", 0]];
  },
  addCommands() {
    return {
      insertProperties:
        () =>
        ({ chain }) =>
          chain()
            .insertContent({ type: this.name, content: t.editor.properties.starterKeys.map((key) => ({ type: PROPERTY_ROW_NODE, attrs: { key } })) })
            // The caret goes to the first value of the table it was left in.
            .command(({ tr }) => {
              let table = -1;
              tr.doc.nodesBetween(0, tr.selection.from, (node, pos) => {
                if (node.type.name === this.name) table = pos;
              });
              if (table >= 0) tr.setSelection(TextSelection.create(tr.doc, table + 2));
              return true;
            })
            .run(),
    };
  },
});
