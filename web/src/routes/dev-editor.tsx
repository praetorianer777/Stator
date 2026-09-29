import { useState } from "react";
import { createRoute } from "@tanstack/react-router";
import { PageHeader, SectionTitle } from "@/components/ui";
import { DocView } from "@/features/editor/DocView";
import { Editor } from "@/features/editor/Editor";
import type { Doc } from "@/features/editor/schema";
import { t } from "@/i18n";
import { appRoute } from "./app";

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
    { type: "codeBlock", attrs: { language: "go" }, content: [{ type: "text", text: 'fmt.Println("hello")' }] },
  ],
};

// Pages do not exist yet, so the editor is reachable here for the browser
// suite; nothing in the navigation links to it.
function DevEditor() {
  const [doc, setDoc] = useState<Doc | null>(sample);
  return (
    <>
      <PageHeader title={t.devEditor.title} />
      <div className="space-y-6" data-dev-editor>
        <Editor id="dev-editor" value={sample} onChange={setDoc} />
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

export const devEditorRoute = createRoute({ getParentRoute: () => appRoute, path: "/dev/editor", component: DevEditor });
