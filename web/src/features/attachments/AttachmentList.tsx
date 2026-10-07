import { useRef, useState, type ReactNode } from "react";
import { attachmentUrl, canPreview, formatSize, useAttachments, useUploadAttachments, type Attachment } from "@/api/attachments";
import { Button, ErrorBanner } from "@/components/ui";
import { Icon } from "@/components/icons";
import { t } from "@/i18n";
import { localDateFormat } from "@/lib/format";
import { PreviewButton } from "./PreviewDialog";
import { byName } from "./versions";

const day = localDateFormat({ dateStyle: "medium" });

function FileLink({ file, label }: { file: Attachment; label: string }) {
  const preview = canPreview(file.contentType);
  return (
    <a
      href={attachmentUrl(file.id, preview)}
      target={preview ? "_blank" : undefined}
      rel="noreferrer"
      download={preview ? undefined : file.fileName}
      className="doc-page-list-title break-all hover:underline"
      aria-label={preview ? t.attachments.open(label) : t.attachments.download(label)}
    >
      {label}
    </a>
  );
}

function meta(file: Attachment): string {
  return [
    formatSize(file.size),
    t.attachmentList.version(file.version),
    t.attachments.uploadedBy(file.uploadedByName || t.attachmentList.someone, day.format(new Date(file.createdAt))),
  ].join(" · ");
}

/**
 * The files of a page in its content, the latest version of each name with
 * the earlier ones a click away; whoever may edit the page uploads here too.
 */
export function AttachmentList({ pageId, editable }: { pageId: string | undefined; editable: boolean }) {
  const l = t.attachmentList;
  const files = useAttachments(pageId);
  const { upload, pending, errors, clearErrors } = useUploadAttachments(pageId ?? "");
  const input = useRef<HTMLInputElement>(null);
  const [notice, setNotice] = useState("");

  let state: string;
  let body: ReactNode;
  if (!pageId) {
    // A page not saved yet has no files to list; the read view always has one.
    state = "unsaved";
    body = <p className="doc-block-empty">{l.unsaved}</p>;
  } else if (files.isPending) {
    state = "loading";
    body = (
      <p className="doc-block-empty" role="status">
        {t.attachments.loading}
      </p>
    );
  } else if (files.isError) {
    state = "failed";
    body = <p className="doc-block-empty">{l.failed}</p>;
  } else if (files.data.length === 0) {
    state = "empty";
    body = <p className="doc-block-empty">{editable ? l.emptyEditable : l.empty}</p>;
  } else {
    state = "list";
    const named = byName(files.data);
    const latestOfEach = named.map((each) => each.latest);
    body = (
      <ul className="doc-page-list">
        {named.map(({ latest, earlier }) => (
          <li key={latest.id} data-listed-file={latest.fileName} data-version={latest.version}>
            <span className="flex items-center gap-1">
              <FileLink file={latest} label={latest.fileName} />
              <PreviewButton file={latest} media={latestOfEach} />
            </span>
            <span className="doc-page-list-meta">{meta(latest)}</span>
            {earlier.length > 0 && (
              <details className="doc-file-versions">
                <summary className="doc-page-list-meta">{l.earlier(earlier.length)}</summary>
                <ul>
                  {earlier.map((file) => (
                    <li key={file.id} data-earlier-version={file.version}>
                      <FileLink file={file} label={l.versionOf(file.fileName, file.version)} />
                      <PreviewButton file={file} label={l.versionOf(file.fileName, file.version)} />
                      <span className="doc-page-list-meta"> {meta(file)}</span>
                    </li>
                  ))}
                </ul>
              </details>
            )}
          </li>
        ))}
      </ul>
    );
  }

  return (
    <section className="doc-page-list-block" aria-label={l.title} data-attachment-list="" data-state={state}>
      <div className="flex flex-wrap items-center justify-between gap-2">
        <p className="doc-chart-title">{l.title}</p>
        {editable && pageId && (
          <>
            <input
              ref={input}
              type="file"
              multiple
              className="sr-only"
              tabIndex={-1}
              aria-hidden="true"
              onChange={(event) => {
                const chosen = [...(event.target.files ?? [])];
                event.target.value = "";
                clearErrors();
                for (const file of chosen) void upload(file).then((made) => made && setNotice(l.uploaded(made.fileName, made.version)));
              }}
              data-attachment-list-input=""
            />
            <Button size="sm" variant="secondary" icon={<Icon.Paperclip />} onClick={() => input.current?.click()} data-action="upload-to-list">
              {l.upload}
            </Button>
          </>
        )}
      </div>
      {editable && pageId && <p className="doc-page-list-meta mb-2">{l.uploadHint}</p>}
      {errors.map((message) => (
        <ErrorBanner key={message}>{message}</ErrorBanner>
      ))}
      {pending.map((each) => {
        const percent = Math.round(each.progress * 100);
        const label = t.attachments.uploading(each.fileName, percent);
        return <progress key={each.key} className="h-1.5 w-full accent-accent" max={100} value={percent} aria-label={label} />;
      })}
      {body}
      <p role="status" className="sr-only">
        {notice}
      </p>
    </section>
  );
}
