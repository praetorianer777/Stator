import type { Attachment } from "@/api/attachments";

/** A file of a page by its name: its latest version, then the earlier ones, newest first. */
export interface FileVersions {
  latest: Attachment;
  earlier: Attachment[];
}

/**
 * The page's files by name, as the server counts versions: whatever the case.
 * Files come latest first, so a name's first file is its latest version.
 */
export function byName(files: Attachment[]): FileVersions[] {
  const groups = new Map<string, FileVersions>();
  for (const file of files) {
    const key = file.fileName.toLowerCase();
    const group = groups.get(key);
    if (group) group.earlier.push(file);
    else groups.set(key, { latest: file, earlier: [] });
  }
  return [...groups.values()];
}
