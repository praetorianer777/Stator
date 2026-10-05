import { useEffect, useState } from "react";
import { attachmentUrl, fetchPreview, hasPreview, previewUrl, type Attachment } from "@/api/attachments";
import { ApiError } from "@/api/client";
import { ButtonLink, Dialog, ErrorBanner, IconButton } from "@/components/ui";
import { Icon } from "@/components/icons";
import { t } from "@/i18n";

type Shown = { state: "loading" } | { state: "ready"; url: string } | { state: "failed"; message: string };

/**
 * A file shown in place as a PDF. The PDF is fetched first and framed from a
 * blob of its own, so a refusal reads as a sentence rather than in the frame.
 */
export function PreviewDialog({ file, onClose }: { file: Attachment; onClose: () => void }) {
  const p = t.preview;
  const [shown, setShown] = useState<Shown>({ state: "loading" });

  useEffect(() => {
    // Not aborted on closing: the server finishes a conversion it began and
    // keeps it, so the next reader waits for nothing.
    let closed = false;
    let url: string | undefined;
    setShown({ state: "loading" });
    fetchPreview(file.id).then(
      (pdf) => {
        if (closed) return;
        url = URL.createObjectURL(pdf);
        setShown({ state: "ready", url });
      },
      (error: unknown) => {
        if (closed) return;
        setShown({ state: "failed", message: error instanceof ApiError && error.message ? error.message : p.failed });
      },
    );
    return () => {
      closed = true;
      if (url) URL.revokeObjectURL(url);
    };
  }, [file.id, p.failed]);

  return (
    <Dialog title={p.title(file.fileName)} fill onClose={onClose} data-preview-dialog={file.id}>
      {shown.state === "ready" ? (
        <iframe
          src={shown.url}
          title={p.frame(file.fileName)}
          className="h-[70vh] w-full rounded-control border border-border bg-surface"
          data-preview-frame=""
        />
      ) : shown.state === "failed" ? (
        <ErrorBanner>{shown.message}</ErrorBanner>
      ) : (
        <p role="status" className="flex h-[70vh] items-center justify-center text-sm text-ink-muted" data-preview-loading="">
          {file.preview === "office" ? p.converting(file.fileName) : p.loading(file.fileName)}
        </p>
      )}
      <div className="mt-3 flex flex-wrap justify-end gap-2">
        {shown.state === "ready" && (
          <ButtonLink href={previewUrl(file.id)} target="_blank" rel="noreferrer" size="sm" data-action="open-preview">
            {p.newTab}
          </ButtonLink>
        )}
        <ButtonLink href={attachmentUrl(file.id)} download={file.fileName} size="sm" icon={<Icon.Download />} data-action="download-previewed">
          {t.attachments.download(file.fileName)}
        </ButtonLink>
      </div>
    </Dialog>
  );
}

/** The button that opens a file's preview, for the files the server can show as a PDF. */
export function PreviewButton({ file, label }: { file: Attachment; label?: string }) {
  const [open, setOpen] = useState(false);
  if (!hasPreview(file)) return null;
  const name = label ?? file.fileName;
  return (
    <>
      <IconButton icon={<Icon.Eye />} label={t.preview.open(name)} size="sm" onClick={() => setOpen(true)} data-action="preview-attachment" />
      {open && <PreviewDialog file={file} onClose={() => setOpen(false)} />}
    </>
  );
}
