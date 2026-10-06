import { attachmentUrl, isImage, isVideo, type Attachment } from "@/api/attachments";

export type MediaKind = "image" | "video";

/**
 * One picture or video a lightbox shows. Its addresses are the reader's own
 * reads of the file, so it shows nothing its reader could not download.
 */
export interface LightboxItem {
  id: string;
  /** What the lightbox is titled while this is shown. */
  name: string;
  kind: MediaKind;
  /** Where the bytes show in place. */
  src: string;
  /** Where they download from. */
  download: string;
  /** The name a download is saved under; empty leaves it to the server. */
  downloadName: string;
  /** The picture's alternative text, when it has its own. */
  alt?: string;
}

/** Whether the lightbox shows a file of this type, and as what. */
export function mediaKind(contentType: string): MediaKind | null {
  if (isImage(contentType)) return "image";
  if (isVideo(contentType)) return "video";
  return null;
}

/** The pictures and videos among a page's files, in their order, as a lightbox shows them. */
export function mediaItems(files: readonly Attachment[], label?: (file: Attachment) => string): LightboxItem[] {
  const items: LightboxItem[] = [];
  for (const file of files) {
    const kind = mediaKind(file.contentType);
    if (!kind) continue;
    items.push({
      id: file.id,
      name: label?.(file) ?? file.fileName,
      kind,
      src: attachmentUrl(file.id, true),
      download: attachmentUrl(file.id),
      downloadName: file.fileName,
    });
  }
  return items;
}
