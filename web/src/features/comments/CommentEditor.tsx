import { useState } from "react";
import { Button, ErrorBanner } from "@/components/ui";
import { COMMENT_EDITOR_ROWS } from "@/config";
import { Editor } from "@/features/editor/Editor";
import type { Doc } from "@/features/editor/schema";
import { t } from "@/i18n";

export interface CommentEditorProps {
  id: string;
  label: string;
  placeholder: string;
  submitLabel: string;
  initial?: Doc | null;
  busy: boolean;
  error?: string;
  onSubmit: (body: Doc) => void;
  onCancel?: () => void;
}

/**
 * The page's editor held to what a comment may hold, with its buttons. It
 * arrives with the first comment somebody writes, so a reader never loads it.
 */
export default function CommentEditor({ id, label, placeholder, submitLabel, initial = null, busy, error, onSubmit, onCancel }: CommentEditorProps) {
  const [body, setBody] = useState<Doc | null>(initial);
  const submit = () => {
    if (body && !busy) onSubmit(body);
  };
  return (
    <form
      className="space-y-2"
      data-comment-editor={id}
      onSubmit={(event) => {
        event.preventDefault();
        submit();
      }}
      onKeyDown={(event) => {
        // While the mention list is open, Escape closes the list alone; the
        // editor names the list it controls then.
        if (event.key === "Escape" && onCancel && !document.getElementById(id)?.hasAttribute("aria-controls")) {
          event.preventDefault();
          onCancel();
        }
      }}
    >
      <Editor
        id={id}
        variant="comment"
        value={initial}
        onChange={setBody}
        rows={COMMENT_EDITOR_ROWS}
        placeholder={placeholder}
        aria-label={label}
        autoFocus
        onSubmit={submit}
      />
      {error && <ErrorBanner>{error}</ErrorBanner>}
      <div className="flex items-center gap-2">
        <Button type="submit" size="sm" loading={busy} disabled={!body} data-action="post-comment">
          {submitLabel}
        </Button>
        {onCancel && (
          <Button type="button" size="sm" variant="ghost" onClick={onCancel} data-action="cancel-comment">
            {t.comments.cancel}
          </Button>
        )}
      </div>
    </form>
  );
}
