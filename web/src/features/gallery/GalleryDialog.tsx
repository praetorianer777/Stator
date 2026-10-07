import { useId, useRef, useState, type FormEvent } from "react";
import { attachmentUrl, isImage, useAttachments, useUploadAttachments, type Attachment } from "@/api/attachments";
import { Button, Checkbox, Dialog, ErrorBanner, Field, IconButton, Select } from "@/components/ui";
import { Icon } from "@/components/icons";
import { GALLERY_CAPTION_MAX_LENGTH, GALLERY_COLUMNS, GALLERY_MAX_IMAGES } from "@/config";
import { byName } from "@/features/attachments/versions";
import { t } from "@/i18n";
import { addPictures, movePicture, type GalleryPicture, type GallerySettings } from "./gallery";

// The types a browser shows in place, which the API takes as pictures.
const PICTURE_TYPES = "image/png,image/jpeg,image/gif,image/webp";

/**
 * Picks a gallery's pictures from the page's files or new uploads, puts them
 * in order with their captions, and says how many go in a row.
 */
export function GalleryDialog({
  initial,
  pageId,
  isNew,
  onSave,
  onClose,
}: {
  initial: GallerySettings;
  /** The page the pictures are files of; none before it is saved. */
  pageId: string | undefined;
  isNew: boolean;
  onSave: (settings: GallerySettings) => void;
  onClose: () => void;
}) {
  const g = t.gallery.dialog;
  const files = useAttachments(pageId);
  const { upload, pending, errors } = useUploadAttachments(pageId ?? "");
  // A gallery pasted together may name a file twice; the dialog lists each once.
  const [pictures, setPictures] = useState<GalleryPicture[]>(() =>
    initial.pictures.filter((p, i, all) => all.findIndex((q) => q.attachmentId === p.attachmentId) === i),
  );
  const [columns, setColumns] = useState(initial.columns);
  const [problem, setProblem] = useState<string | null>(null);
  const input = useRef<HTMLInputElement>(null);
  const ids = useId();

  const all = files.data ?? [];
  const offered = byName(all)
    .map((each) => each.latest)
    .filter((file) => isImage(file.contentType));
  // An upload is named at once, before the page's list of files is read again.
  const [uploaded, setUploaded] = useState<Readonly<Record<string, string>>>({});
  const nameOf = (id: string) => uploaded[id] ?? all.find((file) => file.id === id)?.fileName ?? t.lightbox.picture;
  const chosen = (id: string) => pictures.some((p) => p.attachmentId === id);
  const full = pictures.length >= GALLERY_MAX_IMAGES;

  function toggle(file: Attachment, on: boolean) {
    setProblem(null);
    setPictures((was) => (on ? addPictures(was, [file.id]) : was.filter((p) => p.attachmentId !== file.id)));
  }

  async function uploadPictures(list: FileList | null) {
    if (!list?.length || !pageId) return;
    setProblem(null);
    const made = (await Promise.all([...list].map((file) => upload(file)))).filter((file) => file !== null);
    setUploaded((was) => ({ ...was, ...Object.fromEntries(made.map((file) => [file.id, file.fileName])) }));
    setPictures((was) =>
      addPictures(
        was,
        made.map((file) => file.id),
      ),
    );
  }

  function submit(event: FormEvent) {
    event.preventDefault();
    // The page's own form is this one's ancestor in React's tree.
    event.stopPropagation();
    if (pictures.length === 0) {
      setProblem(g.nonePicked);
      return;
    }
    onSave({ columns, pictures });
  }

  const move = (from: number, to: number) => setPictures((was) => movePicture(was, from, to));

  return (
    <Dialog title={isNew ? g.titleNew : g.titleEdit} wide onClose={onClose} data-gallery-dialog="">
      <form onSubmit={submit} className="space-y-4" noValidate>
        {!pageId ? (
          <p className="text-sm text-ink-muted">{g.unsaved}</p>
        ) : (
          <fieldset className="space-y-2">
            <legend className="text-sm font-medium text-ink-muted">{g.fromPage}</legend>
            {files.isPending && (
              <p className="text-sm text-ink-muted" role="status">
                {t.attachments.loading}
              </p>
            )}
            {files.isError && <ErrorBanner>{g.failed}</ErrorBanner>}
            {files.isSuccess && offered.length === 0 && <p className="text-sm text-ink-muted">{g.noPictures}</p>}
            {offered.length > 0 && (
              <ul className="grid grid-cols-2 gap-2 sm:grid-cols-3" data-gallery-offered="">
                {offered.map((file) => (
                  <li key={file.id} className="flex min-w-0 items-center gap-2 rounded-control border border-border p-1.5">
                    <img src={attachmentUrl(file.id, true)} alt="" loading="lazy" className="size-10 shrink-0 rounded-control object-cover" />
                    <Checkbox
                      label={<span className="min-w-0 break-all">{file.fileName}</span>}
                      checked={chosen(file.id)}
                      disabled={full && !chosen(file.id)}
                      onChange={(event) => toggle(file, event.target.checked)}
                      className="min-w-0"
                      data-gallery-offer={file.fileName}
                    />
                  </li>
                ))}
              </ul>
            )}
            <div className="flex flex-wrap items-center gap-2">
              <input
                ref={input}
                type="file"
                accept={PICTURE_TYPES}
                multiple
                hidden
                onChange={(event) => {
                  void uploadPictures(event.target.files);
                  event.target.value = "";
                }}
                data-gallery-upload=""
              />
              <Button type="button" size="sm" variant="secondary" icon={<Icon.Upload />} disabled={full} onClick={() => input.current?.click()}>
                {g.upload}
              </Button>
              {pending.map((each) => (
                <span key={each.key} role="status" className="text-sm text-ink-muted">
                  {t.attachments.uploading(each.fileName, Math.round(each.progress * 100))}
                </span>
              ))}
            </div>
            {errors.map((message) => (
              <ErrorBanner key={message}>{message}</ErrorBanner>
            ))}
          </fieldset>
        )}

        <fieldset className="space-y-2">
          <legend className="text-sm font-medium text-ink-muted">{g.chosen(pictures.length, GALLERY_MAX_IMAGES)}</legend>
          {pictures.length === 0 ? (
            <p className="text-sm text-ink-muted">{g.chosenEmpty}</p>
          ) : (
            <ol className="space-y-2" data-gallery-chosen="">
              {pictures.map((picture, i) => {
                const name = nameOf(picture.attachmentId);
                return (
                  <li key={picture.attachmentId} className="flex items-start gap-2 rounded-control border border-border p-2" data-gallery-chosen-picture={name}>
                    <img src={attachmentUrl(picture.attachmentId, true)} alt="" loading="lazy" className="size-14 shrink-0 rounded-control object-cover" />
                    <div className="min-w-0 flex-1">
                      <Field
                        id={`${ids}-caption-${i}`}
                        label={g.caption(name)}
                        value={picture.caption ?? ""}
                        maxLength={GALLERY_CAPTION_MAX_LENGTH}
                        controlSize="sm"
                        onChange={(event) => {
                          const caption = event.target.value;
                          setPictures((was) => was.map((p, at) => (at === i ? { ...p, caption } : p)));
                        }}
                      />
                    </div>
                    <div className="flex shrink-0 flex-col gap-0.5 sm:flex-row">
                      <IconButton
                        icon={<Icon.ChevronUp />}
                        label={g.moveUp(name)}
                        size="sm"
                        disabled={i === 0}
                        onClick={() => move(i, i - 1)}
                        data-action="gallery-move-up"
                      />
                      <IconButton
                        icon={<Icon.ChevronDown />}
                        label={g.moveDown(name)}
                        size="sm"
                        disabled={i === pictures.length - 1}
                        onClick={() => move(i, i + 1)}
                        data-action="gallery-move-down"
                      />
                      <IconButton
                        icon={<Icon.Trash />}
                        label={g.remove(name)}
                        size="sm"
                        onClick={() => setPictures((was) => was.filter((_, at) => at !== i))}
                        data-action="gallery-remove"
                      />
                    </div>
                  </li>
                );
              })}
            </ol>
          )}
        </fieldset>

        <Select label={g.columns} hint={g.columnsHint} value={columns} onChange={(event) => setColumns(Number(event.target.value))}>
          {GALLERY_COLUMNS.map((n) => (
            <option key={n} value={n}>
              {g.perRow(n)}
            </option>
          ))}
        </Select>
        {problem && <ErrorBanner>{problem}</ErrorBanner>}
        <div className="flex justify-end gap-2 pt-1">
          <Button type="button" variant="secondary" onClick={onClose}>
            {g.cancel}
          </Button>
          <Button type="submit" disabled={pending.length > 0} data-action="save-gallery">
            {isNew ? g.insert : g.save}
          </Button>
        </div>
      </form>
    </Dialog>
  );
}
