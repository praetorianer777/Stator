import { createContext, useContext, useEffect, useState } from "react";

/**
 * Which attachments a page still has, so a node that names a deleted one is
 * drawn as missing. Undefined while nobody knows yet: then every file is drawn.
 */
export type KnownAttachments = ReadonlySet<string> | undefined;

export const KnownAttachmentsContext = createContext<KnownAttachments>(undefined);

export function useKnownAttachments(): KnownAttachments {
  return useContext(KnownAttachmentsContext);
}

/** Whether an attachment is known to be gone; unknown is not gone. */
export function isMissing(known: KnownAttachments, id: unknown): boolean {
  return known !== undefined && (typeof id !== "string" || !known.has(id));
}

/**
 * The same knowledge for the editor's node views, which ProseMirror draws
 * outside React and which have to be told when the list changes.
 */
export interface AttachmentIndex {
  known: () => KnownAttachments;
  subscribe: (listener: () => void) => () => void;
}

export function createAttachmentIndex(): AttachmentIndex & { set: (ids: KnownAttachments) => void } {
  let current: KnownAttachments;
  const listeners = new Set<() => void>();
  return {
    known: () => current,
    set: (ids) => {
      current = ids;
      for (const listener of listeners) listener();
    },
    subscribe: (listener) => {
      listeners.add(listener);
      return () => listeners.delete(listener);
    },
  };
}

/** An index for the editor that follows a list of ids as it loads and changes. */
export function useAttachmentIndex(ids: readonly string[] | undefined): AttachmentIndex {
  const [index] = useState(createAttachmentIndex);
  const key = ids?.join(",");
  useEffect(() => {
    index.set(key === undefined ? undefined : new Set(key ? key.split(",") : []));
  }, [index, key]);
  return index;
}
