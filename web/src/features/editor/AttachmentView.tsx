import { useState } from "react";
import { attachmentUrl } from "@/api/attachments";
import { publicAttachmentUrl } from "@/api/public";
import { Icon } from "@/components/icons";
import { t } from "@/i18n";
import { isMissing, useKnownAttachments } from "./attachmentIndex";
import { ATTACHMENT_ID_PATTERN, imageAlt, imageWidth } from "./attachments";
import { usePublicReading } from "./publicReading";
import type { DocNode } from "./schema";

/** Where a file downloads from, or shows in place: the public reads for somebody who is not signed in. */
function useFileUrl(): (id: string, inline?: boolean) => string {
  const org = usePublicReading();
  return org ? (id, inline) => publicAttachmentUrl(org, id, inline) : attachmentUrl;
}

function idOf(node: DocNode): string | null {
  const id = node.attrs?.attachmentId;
  return typeof id === "string" && ATTACHMENT_ID_PATTERN.test(id) ? id : null;
}

/** A page's picture as the reader sees it, or a note in its place once the file is gone. */
export function DocImage({ node }: { node: DocNode }) {
  const known = useKnownAttachments();
  const url = useFileUrl();
  const [broken, setBroken] = useState(false);
  const id = idOf(node);
  if (!id || broken || isMissing(known, id)) {
    return (
      <figure data-image data-image-missing="">
        <p className="doc-missing-file">{t.attachments.missingImage}</p>
      </figure>
    );
  }
  const width = imageWidth(node.attrs?.width);
  return (
    <figure data-image data-attachment-id={id}>
      <img src={url(id, true)} alt={imageAlt(node.attrs?.alt) ?? ""} style={width ? { width } : undefined} onError={() => setBroken(true)} />
    </figure>
  );
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
  return (
    <a href={url(id)} download={name || true} data-attachment-chip data-attachment-id={id} aria-label={t.attachments.download(name)}>
      <Icon.File className="inline align-[-2px]" />
      {name}
    </a>
  );
}
