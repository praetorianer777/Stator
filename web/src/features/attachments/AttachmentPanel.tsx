import { localDateFormat } from "@/lib/format";
import { useRef, useState, type DragEvent, type ReactNode } from "react";
import {
  attachmentUrl,
  canPreview,
  formatSize,
  useAttachments,
  useDeleteAttachment,
  useRestoreAttachment,
  useUploadAttachments,
  type Attachment,
} from "@/api/attachments";
import { Button, ButtonLink, ErrorBanner, IconButton, SectionTitle, cx } from "@/components/ui";
import { Icon } from "@/components/icons";
import { t } from "@/i18n";
import { AnnotateButton } from "@/features/annotate/AnnotateButton";
import { PreviewButton } from "./PreviewDialog";
import { byName, versionNote } from "./versions";

const uploadedAt = localDateFormat({ dateStyle: "medium" });

function hasFiles(event: DragEvent): boolean {
  return Array.from(event.dataTransfer.types).includes("Files");
}

/** One version of a file as the panel lists it: its link, what is known of it, and what may be done to it. */
function FileRow({ file, label, media, children }: { file: Attachment; label: string; media?: readonly Attachment[]; children?: ReactNode }) {
  const preview = canPreview(file.contentType);
  const note = versionNote(file);
  return (
    <>
      <Icon.File className="shrink-0 text-ink-muted" />
      <a
        href={attachmentUrl(file.id, preview)}
        target={preview ? "_blank" : undefined}
        rel="noreferrer"
        download={preview ? undefined : file.fileName}
        className="min-w-0 flex-1 truncate text-ink hover:text-accent"
        aria-label={preview ? t.attachments.open(label) : t.attachments.download(label)}
      >
        {label}
      </a>
      {note && <span className="hidden text-ink-muted sm:inline">{note}</span>}
      <span className="text-ink-muted tabular-nums">{formatSize(file.size)}</span>
      <span className="hidden text-ink-muted sm:inline">{t.attachments.uploadedBy(file.uploadedByName, uploadedAt.format(new Date(file.createdAt)))}</span>
      <PreviewButton file={file} label={label} media={media} />
      <ButtonLink
        href={attachmentUrl(file.id)}
        download={file.fileName}
        variant="ghost"
        size="sm"
        icon={<Icon.Download />}
        aria-label={t.attachments.download(label)}
        title={t.attachments.download(label)}
        data-action="download-attachment"
      />
      {children}
    </>
  );
}

/**
 * The files on a page, the latest version of each name with the earlier ones
 * a click away to download, restore or delete. Uploading goes through the API
 * to storage, by the picker or by dropping onto the panel.
 */
export function AttachmentPanel({ pageId, editable }: { pageId: string; editable: boolean }) {
  const { data: attachments, isLoading, error, refetch } = useAttachments(pageId);
  const { upload, pending, errors, clearErrors } = useUploadAttachments(pageId);
  const remove = useDeleteAttachment(pageId);
  const restore = useRestoreAttachment(pageId);
  const input = useRef<HTMLInputElement>(null);
  const [over, setOver] = useState(false);
  const [notice, setNotice] = useState("");

  const settle = () => {
    clearErrors();
    remove.reset();
    restore.reset();
  };

  const send = (files: File[]) => {
    settle();
    for (const file of files) void upload(file).then((made) => made && setNotice(t.attachments.uploaded(made.fileName)));
  };

  const named = byName(attachments ?? []);
  const media = named.map((each) => each.latest);
  const busy = (id: string) => (remove.isPending && remove.variables?.id === id) || (restore.isPending && restore.variables === id);
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
        <SectionTitle id="attachments-title">{named.length > 0 ? t.attachments.count(named.length) : t.attachments.title}</SectionTitle>
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
        {restore.error && <ErrorBanner>{restore.error.message}</ErrorBanner>}
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
      ) : named.length === 0 ? (
        pending.length === 0 && <p className="text-sm text-ink-muted">{editable ? t.attachments.emptyEditable : t.attachments.empty}</p>
      ) : (
        <ul className="divide-y divide-border rounded-control border border-border">
          {named.map(({ latest, earlier }) => (
            <li key={latest.id} className="px-3 py-2 text-sm" data-attachment={latest.fileName} data-attachment-id={latest.id} data-version={latest.version}>
              <div className="flex items-center gap-3">
                <FileRow file={latest} label={latest.fileName} media={media}>
                  {editable && <AnnotateButton file={latest} onSaved={(made) => setNotice(t.annotate.saved(made.fileName, made.version))} />}
                  {editable && (
                    <IconButton
                      icon={<Icon.Trash />}
                      label={t.attachments.remove(latest.fileName)}
                      size="sm"
                      disabled={busy(latest.id)}
                      onClick={() => {
                        const every = earlier.length > 0;
                        const ask = every ? t.attachments.confirmRemoveAll(latest.fileName, earlier.length + 1) : t.attachments.confirmRemove(latest.fileName);
                        if (!window.confirm(ask)) return;
                        settle();
                        remove.mutate({ id: latest.id, every }, { onSuccess: () => setNotice(t.attachments.removed(latest.fileName)) });
                      }}
                      data-action="delete-attachment"
                    />
                  )}
                </FileRow>
              </div>
              {earlier.length > 0 && (
                <details className="mt-1 pl-7" data-attachment-versions>
                  <summary className="cursor-pointer text-ink-muted hover:text-ink">{t.attachmentList.earlier(earlier.length)}</summary>
                  <ul className="mt-1 space-y-1">
                    {earlier.map((file) => {
                      const label = t.attachmentList.versionOf(file.fileName, file.version);
                      return (
                        <li key={file.id} className="flex items-center gap-3" data-attachment-version={file.version} data-attachment-id={file.id}>
                          <FileRow file={file} label={label}>
                            {editable && (
                              <>
                                <AnnotateButton file={file} label={label} onSaved={(made) => setNotice(t.annotate.saved(made.fileName, made.version))} />
                                <Button
                                  size="sm"
                                  variant="ghost"
                                  disabled={busy(file.id)}
                                  aria-label={t.attachments.restoreVersion(file.fileName, file.version)}
                                  onClick={() => {
                                    settle();
                                    restore.mutate(file.id, {
                                      onSuccess: (made) => setNotice(t.attachments.restored(file.fileName, file.version, made.version)),
                                    });
                                  }}
                                  data-action="restore-attachment"
                                >
                                  {t.attachments.restore}
                                </Button>
                                <IconButton
                                  icon={<Icon.Trash />}
                                  label={t.attachments.removeVersion(file.fileName, file.version)}
                                  size="sm"
                                  disabled={busy(file.id)}
                                  onClick={() => {
                                    if (!window.confirm(t.attachments.confirmRemoveVersion(file.fileName, file.version))) return;
                                    settle();
                                    remove.mutate({ id: file.id }, { onSuccess: () => setNotice(t.attachments.removedVersion(file.fileName, file.version)) });
                                  }}
                                  data-action="delete-attachment-version"
                                />
                              </>
                            )}
                          </FileRow>
                        </li>
                      );
                    })}
                  </ul>
                </details>
              )}
            </li>
          ))}
        </ul>
      )}
      <p role="status" className="sr-only" data-attachment-notice>
        {notice}
      </p>
    </section>
  );
}
