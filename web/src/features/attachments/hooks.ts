import { useMemo } from "react";
import { useAttachments, useUploadAttachments } from "@/api/attachments";
import { useAttachmentIndex, type KnownAttachments } from "@/features/editor/attachmentIndex";

/** The ids of a page's files, for drawing the ones its document names but that are gone. */
export function usePageAttachmentIds(pageId: string): KnownAttachments {
  const { data } = useAttachments(pageId);
  return useMemo(() => data && new Set(data.map((a) => a.id)), [data]);
}

/** What the page editor needs to take files: where they go, which the page has, and what was refused. */
export function useEditorAttachments(pageId: string) {
  const { data } = useAttachments(pageId);
  const { upload, errors, clearErrors } = useUploadAttachments(pageId);
  const index = useAttachmentIndex(useMemo(() => data?.map((a) => a.id), [data]));
  return { upload, index, errors, clearErrors };
}
