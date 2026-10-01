import { localDateFormat } from "@/lib/format";
import { useRef, useState, type DragEvent } from "react";
import { attachmentUrl, canPreview, formatSize, useAttachments, useDeleteAttachment, useUploadAttachments } from "@/api/attachments";
import { Button, ButtonLink, ErrorBanner, IconButton, SectionTitle, cx } from "@/components/ui";
import { Icon } from "@/components/icons";
import { t } from "@/i18n";

const uploadedAt = localDateFormat({ dateStyle: "medium" });

function hasFiles(event: DragEvent): boolean {
  return Array.from(event.dataTransfer.types).includes("Files");
}

/**
 * The files on a page. Uploading goes through the API to storage, by the
 * picker or by dropping onto the panel; the bytes are one link away.
 */
export function AttachmentPanel({ pageId, editable }: { pageId: string; editable: boolean }) {
  const { data: attachments, isLoading, error, refetch } = useAttachments(pageId);
  const { upload, pending, errors, clearErrors } = useUploadAttachments(pageId);
  const remove = useDeleteAttachment(pageId);
  const input = useRef<HTMLInputElement>(null);
  const [over, setOver] = useState(false);
  const [notice, setNotice] = useState("");

  const send = (files: File[]) => {
    clearErrors();
    remove.reset();
    for (const file of files) void upload(file).then((made) => made && setNotice(t.attachments.uploaded(made.fileName)));
  };

  const list = attachments ?? [];
  const drop = editable
    ? {
        onDragEnter: (event: DragEvent) => {
          if (hasFiles(event)) setOver(true);
        },
        onDragOver: (event: DragEvent) => {
          if (!hasFiles(event)) return;
          event.preventDefault();
          event.dataTransfer.dropEffect = "copy";
        },
        onDragLeave: (event: DragEvent) => {
          if (!event.currentTarget.contains(event.relatedTarget as Node | null)) setOver(false);
        },
        onDrop: (event: DragEvent) => {
          if (!hasFiles(event)) return;
          event.preventDefault();
          setOver(false);
          send([...event.dataTransfer.files]);
        },
      }
    : {};

  return (
    <section
      aria-labelledby="attachments-title"
      className={cx("mt-10 rounded-overlay border border-transparent p-3 -mx-3", over && "border-dashed border-accent bg-accent-subtle")}
      data-attachments
      data-drop-active={over || undefined}
      {...drop}
    >
      <div className="mb-3 flex items-center justify-between gap-3">
        <SectionTitle id="attachments-title">{list.length > 0 ? t.attachments.count(list.length) : t.attachments.title}</SectionTitle>
        {editable && (
          <>
            <input
              ref={input}
              type="file"
              multiple
              className="sr-only"
              tabIndex={-1}
              aria-hidden="true"
              onChange={(event) => {
                const files = [...(event.target.files ?? [])];
                event.target.value = "";
                send(files);
              }}
              data-attachment-input
            />
            <Button size="sm" variant="secondary" icon={<Icon.Paperclip />} onClick={() => input.current?.click()} data-action="attach-files">
              {t.attachments.attach}
            </Button>
          </>
        )}
      </div>

      {over && <p className="mb-3 text-sm text-ink">{t.attachments.dropHere}</p>}

      <div className="space-y-2" data-attachment-errors>
        {errors.map((message) => (
          <ErrorBanner key={message}>{message}</ErrorBanner>
        ))}
        {remove.error && <ErrorBanner>{remove.error.message}</ErrorBanner>}
        {error && <ErrorBanner onRetry={() => void refetch()}>{error.message}</ErrorBanner>}
      </div>

      {pending.length > 0 && (
        <ul className="mb-3 space-y-2" data-uploads>
          {pending.map((each) => {
            const percent = Math.round(each.progress * 100);
            const label = t.attachments.uploading(each.fileName, percent);
            return (
              <li key={each.key} className="text-sm" data-upload={each.fileName}>
                <span className="block truncate text-ink-muted">{label}</span>
                <progress className="h-1.5 w-full accent-accent" max={100} value={percent} aria-label={label} />
              </li>
            );
          })}
        </ul>
      )}

      {isLoading ? (
        <p className="text-sm text-ink-muted">{t.attachments.loading}</p>
      ) : list.length === 0 ? (
        pending.length === 0 && <p className="text-sm text-ink-muted">{editable ? t.attachments.emptyEditable : t.attachments.empty}</p>
      ) : (
        <ul className="divide-y divide-border rounded-control border border-border">
          {list.map((a) => {
            const preview = canPreview(a.contentType);
            return (
              <li key={a.id} className="flex items-center gap-3 px-3 py-2 text-sm" data-attachment={a.fileName} data-attachment-id={a.id}>
                <Icon.File className="shrink-0 text-ink-muted" />
                <a
                  href={attachmentUrl(a.id, preview)}
                  target={preview ? "_blank" : undefined}
                  rel="noreferrer"
                  download={preview ? undefined : a.fileName}
                  className="min-w-0 flex-1 truncate text-ink hover:text-accent"
                  aria-label={preview ? t.attachments.open(a.fileName) : t.attachments.download(a.fileName)}
                >
                  {a.fileName}
                </a>
                <span className="text-ink-muted tabular-nums">{formatSize(a.size)}</span>
                <span className="hidden text-ink-muted sm:inline">{t.attachments.uploadedBy(a.uploadedByName, uploadedAt.format(new Date(a.createdAt)))}</span>
                <ButtonLink
                  href={attachmentUrl(a.id)}
                  download={a.fileName}
                  variant="ghost"
                  size="sm"
                  icon={<Icon.Download />}
                  aria-label={t.attachments.download(a.fileName)}
                  title={t.attachments.download(a.fileName)}
                  data-action="download-attachment"
                />
                {editable && (
                  <IconButton
                    icon={<Icon.Trash />}
                    label={t.attachments.remove(a.fileName)}
                    size="sm"
                    disabled={remove.isPending && remove.variables === a.id}
                    onClick={() => {
                      if (!window.confirm(t.attachments.confirmRemove(a.fileName))) return;
                      clearErrors();
                      remove.mutate(a.id, { onSuccess: () => setNotice(t.attachments.removed(a.fileName)) });
                    }}
                    data-action="delete-attachment"
                  />
                )}
              </li>
            );
          })}
        </ul>
      )}
      <p role="status" className="sr-only" data-attachment-notice>
        {notice}
      </p>
    </section>
  );
}
