import { useEffect, useRef, useState } from "react";
import { fetchFile, saveFile } from "@/api/pdf";
import { Button, Dialog, ErrorBanner } from "@/components/ui";
import { Icon } from "@/components/icons";
import { t } from "@/i18n";
import { pageSlug } from "@/lib/slug";

/** The files a page is exported as from its menu: a PDF to print, a Word document to edit offline. */
export type ExportFormat = "pdf" | "docx";

/**
 * Makes a page's file from href while it is open, saves it and closes; an
 * export that fails stays open with the reason and a way to try again.
 */
export function FileExportDialog({ format, href, title, onClose }: { format: ExportFormat; href: string; title: string; onClose: () => void }) {
  const [error, setError] = useState<string>();
  const [attempt, setAttempt] = useState(0);
  const close = useRef(onClose);
  close.current = onClose;
  const words = t[format];

  // biome-ignore lint/correctness/useExhaustiveDependencies: attempt changes on each retry, which is the point
  useEffect(() => {
    // The export runs on once the dialog is closed; only its answer is dropped.
    let live = true;
    setError(undefined);
    fetchFile(href, `${pageSlug(title)}.${format}`, t[format].networkFailed).then(
      (file) => {
        if (!live) return;
        saveFile(file);
        close.current();
      },
      (failure: unknown) => {
        if (live) setError(failure instanceof Error ? failure.message : String(failure));
      },
    );
    return () => {
      live = false;
    };
  }, [format, href, title, attempt]);

  const state = error ? "failed" : "making";
  return (
    <Dialog
      title={words.title(title)}
      onClose={onClose}
      data-pdf-export={format === "pdf" ? state : undefined}
      data-docx-export={format === "docx" ? state : undefined}
    >
      {error ? (
        <ErrorBanner onRetry={() => setAttempt((n) => n + 1)}>{error}</ErrorBanner>
      ) : (
        <p role="status" className="flex items-start gap-2 text-sm text-ink" data-export-making={format}>
          <Icon.Download className="mt-0.5 shrink-0 animate-pulse text-ink-muted motion-reduce:animate-none" aria-hidden="true" />
          <span>{words.making}</span>
        </p>
      )}
      <p className="mt-3 text-xs text-ink-muted">{words.note}</p>
      <div className="flex justify-end pt-3">
        <Button type="button" variant="secondary" onClick={onClose} data-action={`close-${format}`}>
          {error ? words.close : words.cancel}
        </Button>
      </div>
    </Dialog>
  );
}
