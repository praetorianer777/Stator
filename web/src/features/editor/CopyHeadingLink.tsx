import { useCallback, useEffect, useRef, useState } from "react";
import { COPY_FEEDBACK_MS } from "@/config";
import { t } from "@/i18n";
import { headingLink } from "./schema";

/**
 * Copies the address of a heading and says so. The status line is always
 * there, empty until needed, so a screen reader hears the change.
 */
export function useCopyHeadingLink() {
  const [message, setMessage] = useState("");
  const timer = useRef<ReturnType<typeof setTimeout> | undefined>(undefined);
  useEffect(() => () => clearTimeout(timer.current), []);

  const copy = useCallback(async (anchor: string) => {
    let said = t.editor.linkCopied;
    try {
      await navigator.clipboard.writeText(headingLink(anchor));
    } catch {
      said = t.editor.copyFailed;
    }
    setMessage(said);
    clearTimeout(timer.current);
    timer.current = setTimeout(() => setMessage(""), COPY_FEEDBACK_MS);
  }, []);

  const status = (
    <p role="status" className="px-4 text-xs text-ink-muted [&:not(:empty)]:py-1" data-copy-status>
      {message}
    </p>
  );
  return { copy, status };
}
