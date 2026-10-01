import { useState } from "react";
import { createLazyRoute } from "@tanstack/react-router";
import { PageHeader, SectionTitle } from "@/components/ui";
import { DocView } from "@/features/editor/DocView";
import { Editor } from "@/features/editor/Editor";
import type { Doc, Mentionable } from "@/features/editor/schema";
import { t } from "@/i18n";
import { devEditorRoute } from "./dev-editor";

// Between them these reach every colour the highlighting uses, so the
// browser suite can judge each one's contrast on a code block.
const highlighted: Array<[string, string]> = [
  ["typescript", "// Greets whoever asks.\nexport function greet(name: string): string {\n  const times = 3;\n  return 'Hello, '.repeat(times) + name;\n}"],
  ["python", "@dataclass\nclass Point:\n    x: int = 0\n    label = None"],
  ["bash", 'echo "$HOME" | grep -c home'],
  ["css", ".card #title {\n  color: #fff;\n  margin: 4px;\n}"],
  ["json", '{ "name": "stator", "private": true, "port": 8080 }'],
  ["diff", "@@ -1 +1 @@\n-old line\n+new line"],
];

const sample: Doc = {
  type: "doc",
  content: [
    { type: "heading", attrs: { level: 1, id: "getting-started" }, content: [{ type: "text", text: "Getting started" }] },
    {
      type: "paragraph",
      content: [
        { type: "text", text: "Type " },
        { type: "text", text: "/", marks: [{ type: "code" }] },
        { type: "text", text: " for blocks, or write markdown." },
      ],
    },
    {
      type: "panel",
      attrs: { kind: "info" },
      content: [{ type: "paragraph", content: [{ type: "text", text: "Panels take their colours from the theme." }] }],
    },
    {
      type: "expand",
      attrs: { title: "How a custom theme reaches them" },
      content: [{ type: "paragraph", content: [{ type: "text", text: "Every colour is a theme role, so a custom theme recolours them." }] }],
    },
    { type: "codeBlock", attrs: { language: "go" }, content: [{ type: "text", text: 'fmt.Println("hello")' }] },
    ...highlighted.map(([language, text]) => ({ type: "codeBlock", attrs: { language }, content: [{ type: "text", text }] })),
  ],
};

// A page's editor looks its people up; this page has none, so the browser
// suite reaches a mention list of fixed length through these.
const people: Mentionable[] = [
  { id: "0199a1b2-c3d4-7e5f-8a9b-0c1d2e3f4b01", name: "Ada Lovelace", email: "ada@example.test" },
  { id: "0199a1b2-c3d4-7e5f-8a9b-0c1d2e3f4b02", name: "Alan Turing", email: "alan@example.test" },
  { id: "0199a1b2-c3d4-7e5f-8a9b-0c1d2e3f4b03", name: "Alonzo Church", email: "alonzo@example.test" },
  { id: "0199a1b2-c3d4-7e5f-8a9b-0c1d2e3f4b04", name: "Anita Borg", email: "anita@example.test" },
  { id: "0199a1b2-c3d4-7e5f-8a9b-0c1d2e3f4b05", name: "Annie Easley", email: "annie@example.test" },
  { id: "0199a1b2-c3d4-7e5f-8a9b-0c1d2e3f4b06", name: "Adele Goldberg", email: "adele@example.test" },
  { id: "0199a1b2-c3d4-7e5f-8a9b-0c1d2e3f4b07", name: "Andrew Tanenbaum", email: "andrew@example.test" },
  { id: "0199a1b2-c3d4-7e5f-8a9b-0c1d2e3f4b08", name: "Alfred Aho", email: "alfred@example.test" },
];

// Pages do not exist yet, so the editor is reachable here for the browser
// suite; nothing in the navigation links to it.
function DevEditor() {
  const [doc, setDoc] = useState<Doc | null>(sample);
  return (
    <>
      <PageHeader title={t.devEditor.title} />
      <div className="space-y-6" data-dev-editor>
        <Editor id="dev-editor" value={sample} onChange={setDoc} people={people} />
        <section aria-labelledby="dev-editor-preview" className="space-y-2">
          <SectionTitle id="dev-editor-preview">{t.devEditor.preview}</SectionTitle>
          <div data-dev-preview>
            <DocView doc={doc} />
          </div>
        </section>
        <section aria-labelledby="dev-editor-json" className="space-y-2">
          <SectionTitle id="dev-editor-json">{t.devEditor.json}</SectionTitle>
          {/* The box scrolls on its own, so it is a tab stop the arrow keys can scroll. */}
          <pre
            tabIndex={0}
            role="region"
            aria-label={t.devEditor.jsonBox}
            className="max-h-96 overflow-auto rounded-control bg-surface-sunken p-3 font-mono text-xs"
            data-dev-json
          >
            {JSON.stringify(doc, null, 2)}
          </pre>
        </section>
      </div>
    </>
  );
}

export const Route = createLazyRoute(devEditorRoute.id)({ component: DevEditor });
