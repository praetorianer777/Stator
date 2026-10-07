import { useState, type ReactNode } from "react";
import { attachmentUrl } from "@/api/attachments";
import { VIDEO_FILE_PATTERN } from "@/config";
import { IconButton } from "@/components/ui";
import { Lightbox } from "@/features/attachments/Lightbox";
import type { LightboxItem } from "@/features/attachments/lightboxItems";
import { Gallery } from "@/features/gallery/Gallery";
import { galleryOf } from "@/features/gallery/gallery";
import { linkedAttachmentUrl, publicAttachmentUrl } from "@/api/public";
import { Icon } from "@/components/icons";
import { t } from "@/i18n";
import { isMissing, useKnownAttachments } from "./attachmentIndex";
import { ATTACHMENT_ID_PATTERN, imageAlt, imageWidth } from "./attachments";
import { usePublicLink, usePublicReading } from "./publicReading";
import type { DocNode } from "./schema";

/** Where a file downloads from, or shows in place: the public reads, or the link's, for somebody who is not signed in. */
export function useFileUrl(): (id: string, inline?: boolean) => string {
  const org = usePublicReading();
  const token = usePublicLink();
  if (org && token) return (id, inline) => linkedAttachmentUrl(org, token, id, inline);
  return org ? (id, inline) => publicAttachmentUrl(org, id, inline) : attachmentUrl;
}

function idOf(node: DocNode): string | null {
  const id = node.attrs?.attachmentId;
  return typeof id === "string" && ATTACHMENT_ID_PATTERN.test(id) ? id : null;
}

/**
 * A page's picture as the reader sees it, which opens larger in the lightbox,
 * or a note in its place once the file is gone.
 */
export function DocImage({ node }: { node: DocNode }) {
  const known = useKnownAttachments();
  const url = useFileUrl();
  const [broken, setBroken] = useState(false);
  const [open, setOpen] = useState(false);
  const id = idOf(node);
  if (!id || broken || isMissing(known, id)) {
    return (
      <figure data-image data-image-missing="">
        <p className="doc-missing-file">{t.attachments.missingImage}</p>
      </figure>
    );
  }
  const width = imageWidth(node.attrs?.width);
  const alt = imageAlt(node.attrs?.alt) ?? "";
  const item: LightboxItem = { id, name: alt || t.lightbox.picture, kind: "image", src: url(id, true), download: url(id), downloadName: "", alt };
  return (
    <figure data-image data-attachment-id={id}>
      <button type="button" className="doc-image-open" aria-label={t.lightbox.openImage(alt)} onClick={() => setOpen(true)} data-action="open-lightbox">
        <img src={item.src} alt={alt} style={width ? { width } : undefined} onError={() => setBroken(true)} />
      </button>
      {open && <Lightbox items={[item]} onClose={() => setOpen(false)} />}
    </figure>
  );
}

/** A page's gallery as the reader sees it, each picture through the reader's own read of its file. */
export function DocGallery({ node }: { node: DocNode }) {
  return <Gallery settings={galleryOf(node)} url={useFileUrl()} known={useKnownAttachments()} />;
}

/** A file in a line of text: a link that downloads it, or its name marked as deleted. */
export function DocAttachment({ node }: { node: DocNode }) {
  const known = useKnownAttachments();
  const url = useFileUrl();
  const id = idOf(node);
  const name = typeof node.attrs?.fileName === "string" ? node.attrs.fileName : "";
  if (!id || isMissing(known, id)) {
    return (
      <span data-attachment-chip data-missing="" className="doc-missing-file">
        {t.attachments.missing(name)}
      </span>
    );
  }
  const chip = (
    <a href={url(id)} download={name || true} data-attachment-chip data-attachment-id={id} aria-label={t.attachments.download(name)}>
      <Icon.File className="inline align-[-2px]" />
      {name}
    </a>
  );
  if (!VIDEO_FILE_PATTERN.test(name)) return chip;
  return <VideoChip id={id} name={name} url={url} chip={chip} />;
}

// A chip names its file but not its type, so a video is told by its name and
// the player says so in a sentence if the file turns out not to play.
function VideoChip({ id, name, url, chip }: { id: string; name: string; url: (id: string, inline?: boolean) => string; chip: ReactNode }) {
  const [open, setOpen] = useState(false);
  const item: LightboxItem = { id, name, kind: "video", src: url(id, true), download: url(id), downloadName: name };
  return (
    <span className="inline-flex items-center gap-0.5 align-middle" data-video-chip="">
      {chip}
      <IconButton icon={<Icon.Play />} label={t.lightbox.play(name)} size="xs" onClick={() => setOpen(true)} data-action="play-video" />
      {open && <Lightbox items={[item]} onClose={() => setOpen(false)} />}
    </span>
  );
}
