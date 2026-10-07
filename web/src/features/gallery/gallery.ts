import type { JSONContent } from "@tiptap/core";
import { GALLERY_CAPTION_MAX_LENGTH, GALLERY_COLUMNS, GALLERY_DEFAULT_COLUMNS, GALLERY_MAX_IMAGES } from "@/config";
import type { LightboxItem } from "@/features/attachments/lightboxItems";
import { ATTACHMENT_ID_PATTERN } from "@/features/editor/attachments";
import { t } from "@/i18n";

export const GALLERY_NODE = "gallery";
export const GALLERY_IMAGE_NODE = "galleryImage";

/** One picture of a gallery: one version of a file of the page, by id, as an image names it. */
export interface GalleryPicture {
  attachmentId: string;
  caption: string | null;
}

/** What a gallery stores: how many pictures a row holds, and the pictures in their order. */
export interface GallerySettings {
  columns: number;
  pictures: GalleryPicture[];
}

/** A row the API takes, or the default. */
export function galleryColumns(value: unknown): number {
  return typeof value === "number" && (GALLERY_COLUMNS as readonly number[]).includes(value) ? value : GALLERY_DEFAULT_COLUMNS;
}

/** A caption within the API's limit, or none for one left blank. */
export function galleryCaption(value: unknown): string | null {
  if (typeof value !== "string" || value.trim() === "") return null;
  return [...value].slice(0, GALLERY_CAPTION_MAX_LENGTH).join("");
}

/** A gallery node's settings; a picture that names no file the API could have written is left out. */
export function galleryOf(node: Pick<JSONContent, "attrs" | "content">): GallerySettings {
  const pictures: GalleryPicture[] = [];
  for (const child of node.content ?? []) {
    const id = child.attrs?.attachmentId;
    if (child.type === GALLERY_IMAGE_NODE && typeof id === "string" && ATTACHMENT_ID_PATTERN.test(id)) {
      pictures.push({ attachmentId: id, caption: galleryCaption(child.attrs?.caption) });
    }
  }
  return { columns: galleryColumns(node.attrs?.columns), pictures };
}

/** The node a gallery is stored as; null when it would hold no picture, which the API refuses. */
export function galleryJSON({ columns, pictures }: GallerySettings): JSONContent | null {
  if (pictures.length === 0) return null;
  return {
    type: GALLERY_NODE,
    attrs: { columns: galleryColumns(columns) },
    content: pictures.slice(0, GALLERY_MAX_IMAGES).map((p) => ({
      type: GALLERY_IMAGE_NODE,
      attrs: { attachmentId: p.attachmentId, caption: galleryCaption(p.caption) },
    })),
  };
}

/** The pictures with one moved from one place to another; a place outside the list changes nothing. */
export function movePicture<T>(pictures: readonly T[], from: number, to: number): T[] {
  if (from === to || from < 0 || to < 0 || from >= pictures.length || to >= pictures.length) return [...pictures];
  const out = [...pictures];
  const [moved] = out.splice(from, 1);
  out.splice(to, 0, moved!);
  return out;
}

/** The pictures with files added at the end, each once, up to the most a gallery holds. */
export function addPictures(pictures: readonly GalleryPicture[], ids: readonly string[]): GalleryPicture[] {
  const out = [...pictures];
  for (const id of ids) {
    if (out.length >= GALLERY_MAX_IMAGES) break;
    if (!out.some((p) => p.attachmentId === id)) out.push({ attachmentId: id, caption: null });
  }
  return out;
}

/** Where a file shows in place and downloads from, for the one reading. */
export type FileUrl = (id: string, inline?: boolean) => string;

/** A picture as the lightbox shows it, named by its caption. */
export function pictureItem(picture: GalleryPicture, url: FileUrl): LightboxItem {
  const caption = picture.caption ?? "";
  return {
    id: picture.attachmentId,
    name: caption || t.lightbox.picture,
    kind: "image",
    src: url(picture.attachmentId, true),
    download: url(picture.attachmentId),
    downloadName: "",
    alt: caption,
  };
}
