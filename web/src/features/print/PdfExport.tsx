import { useEffect, useRef, useState } from "react";
import { fetchPdf, saveFile } from "@/api/pdf";
import { Button, Dialog, ErrorBanner } from "@/components/ui";
import { Icon } from "@/components/icons";
import { t } from "@/i18n";
import { pageSlug } from "@/lib/slug";

/**
 * Prints a page as PDF from href while it is open, saves the file and closes;
 * a print that fails stays open with the reason and a way to try again.
 */
export function PdfExportDialog({ href, title, onClose }: { href: string; title: string; onClose: () => void }) {
  const [error, setError] = useState<string>();
  const [attempt, setAttempt] = useState(0);
  const close = useRef(onClose);
  close.current = onClose;

  // biome-ignore lint/correctness/useExhaustiveDependencies: attempt changes on each retry, which is the point
  useEffect(() => {
    // The print runs on once the dialog is closed; only its answer is dropped.
    let live = true;
    setError(undefined);
    fetchPdf(href, `${pageSlug(title)}.pdf`).then(
      (pdf) => {
        if (!live) return;
        saveFile(pdf);
        close.current();
      },
      (failure: unknown) => {
        if (live) setError(failure instanceof Error ? failure.message : String(failure));
      },
    );
    return () => {
      live = false;
    };
  }, [href, title, attempt]);

  return (
    <Dialog title={t.pdf.title(title)} onClose={onClose} data-pdf-export={error ? "failed" : "making"}>
      {error ? (
        <ErrorBanner onRetry={() => setAttempt((n) => n + 1)}>{error}</ErrorBanner>
      ) : (
        <p role="status" className="flex items-start gap-2 text-sm text-ink" data-pdf-making="">
          <Icon.Download className="mt-0.5 shrink-0 animate-pulse text-ink-muted motion-reduce:animate-none" aria-hidden="true" />
          <span>{t.pdf.making}</span>
        </p>
      )}
      <p className="mt-3 text-xs text-ink-muted">{t.pdf.note}</p>
      <div className="flex justify-end pt-3">
        <Button type="button" variant="secondary" onClick={onClose} data-action="close-pdf">
          {error ? t.pdf.close : t.pdf.cancel}
        </Button>
      </div>
    </Dialog>
  );
}
