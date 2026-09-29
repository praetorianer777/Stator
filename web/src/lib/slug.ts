import { PAGE_SLUG_FALLBACK, PAGE_SLUG_MAX_LENGTH } from "@/config";

const WORD_CHAR = /[\p{L}\p{N}]/u;

/**
 * A page's title as the readable end of its address. Only the id finds the
 * page, so a stale slug still opens it and is then put right.
 */
export function pageSlug(title: string): string {
  let out = "";
  let gap = false;
  for (const ch of title.toLowerCase()) {
    if (!WORD_CHAR.test(ch)) {
      gap = true;
      continue;
    }
    const next = gap && out ? `${out}-${ch}` : out + ch;
    if ([...next].length > PAGE_SLUG_MAX_LENGTH) break;
    out = next;
    gap = false;
  }
  return out || PAGE_SLUG_FALLBACK;
}
