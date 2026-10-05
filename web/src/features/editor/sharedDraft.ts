import { getSchema, type JSONContent } from "@tiptap/core";
import type { Schema } from "@tiptap/pm/model";
import { prosemirrorJSONToYXmlFragment, yXmlFragmentToProsemirrorJSON } from "@tiptap/y-tiptap";
import type * as Y from "yjs";
import { BODY_FIELD, titleOf } from "@/features/collab/shared";
import { editorExtensions } from "./extensions";
import { emptyDoc, type Doc } from "./schema";

let schema: Schema | null = null;

/** The page editor's schema, which a shared draft's body is written in. */
function pageSchema(): Schema {
  schema ??= getSchema(editorExtensions({ variant: "page" }));
  return schema;
}

/** Writes a title and a body into an empty shared draft. */
export function fillSharedDraft(doc: Y.Doc, title: string, body: Doc | null) {
  const content = body?.content?.length ? body : emptyDoc;
  prosemirrorJSONToYXmlFragment(pageSchema(), content as JSONContent, doc.getXmlFragment(BODY_FIELD));
  titleOf(doc).insert(0, title);
}

/** A shared draft's body as a document, before an editor has drawn it. */
export function sharedBody(doc: Y.Doc): Doc {
  return yXmlFragmentToProsemirrorJSON(doc.getXmlFragment(BODY_FIELD)) as Doc;
}
