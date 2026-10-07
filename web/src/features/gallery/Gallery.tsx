import { useState, type CSSProperties } from "react";
import { Lightbox } from "@/features/attachments/Lightbox";
import { isMissing, type KnownAttachments } from "@/features/editor/attachmentIndex";
import { t } from "@/i18n";
import { pictureItem, type FileUrl, type GallerySettings } from "./gallery";

/**
 * A gallery's pictures in a grid, each opening the lightbox to step through
 * the others. A picture the reader may not download, or that is gone, is
 * left out; an author editing sees it as a gap to remove.
 */
export function Gallery({
  settings,
  url,
  known,
  editing = false,
}: {
  settings: GallerySettings;
  url: FileUrl;
  known: KnownAttachments;
  /** In the editor a click selects the block, and the gaps stay to be removed. */
  editing?: boolean;
}) {
  const [broken, setBroken] = useState<ReadonlySet<string>>(() => new Set());
  const [open, setOpen] = useState<number | null>(null);
  const shows = (id: string) => !broken.has(id) && !isMissing(known, id);
  const shown = settings.pictures.filter((p) => shows(p.attachmentId));
  const items = shown.map((p) => pictureItem(p, url));
  const listed = editing ? settings.pictures : shown;
  if (listed.length === 0) {
    return (
      <p className="doc-block doc-block-empty" data-gallery="" data-gallery-empty="">
        {t.gallery.nothingShown}
      </p>
    );
  }
  const style = { "--gallery-columns": settings.columns } as CSSProperties;
  let place = 0;
  return (
    <ul className="doc-gallery" style={style} data-gallery="" data-columns={settings.columns} aria-label={t.gallery.label(shown.length)}>
      {listed.map((picture, i) => {
        const caption = picture.caption ?? "";
        if (!shows(picture.attachmentId)) {
          return (
            // biome-ignore lint/suspicious/noArrayIndexKey: a gallery may name a file twice, and only its place tells them apart
            <li key={`${picture.attachmentId}-${i}`} className="doc-gallery-item" data-gallery-missing="">
              <p className="doc-gallery-gap">{t.gallery.missing}</p>
              {caption && <p className="doc-gallery-caption">{caption}</p>}
            </li>
          );
        }
        const at = place++;
        const img = (
          <img
            src={url(picture.attachmentId, true)}
            // The caption is written beside it and the button names it; only an editor's picture without one needs a name.
            alt={editing && !caption ? t.lightbox.picture : ""}
            loading="lazy"
            decoding="async"
            onError={() => setBroken((was) => new Set(was).add(picture.attachmentId))}
          />
        );
        return (
          // biome-ignore lint/suspicious/noArrayIndexKey: a gallery may name a file twice, and only its place tells them apart
          <li key={`${picture.attachmentId}-${i}`} className="doc-gallery-item" data-gallery-picture={picture.attachmentId}>
            <figure>
              {editing ? (
                <div className="doc-gallery-frame">{img}</div>
              ) : (
                <button
                  type="button"
                  className="doc-gallery-frame doc-gallery-open"
                  aria-label={t.gallery.open(caption, at + 1, shown.length)}
                  onClick={() => setOpen(at)}
                  data-action="open-gallery-picture"
                >
                  {img}
                </button>
              )}
              {caption && <figcaption className="doc-gallery-caption">{caption}</figcaption>}
            </figure>
          </li>
        );
      })}
      {open !== null && items.length > 0 && <Lightbox items={items} start={open} onClose={() => setOpen(null)} />}
    </ul>
  );
}
