import { ARMATURE_RECENT_ISSUES, ARMATURE_RECENT_STORAGE_PREFIX } from "@/config";
import { normalizeKey } from "./issueKeys";

// Only keys are kept: what an issue says is read afresh, as the person may
// see it now, so a browser never shows a summary from before.

/** The keys of the issues a person inserted last in this browser, newest first. */
export function recentIssueKeys(userId: string | undefined): string[] {
  if (!userId) return [];
  try {
    const parsed: unknown = JSON.parse(window.localStorage.getItem(ARMATURE_RECENT_STORAGE_PREFIX + userId) ?? "[]");
    if (!Array.isArray(parsed)) return [];
    const keys = parsed.map(normalizeKey).filter((key): key is string => key !== null);
    return [...new Set(keys)].slice(0, ARMATURE_RECENT_ISSUES);
  } catch {
    return [];
  }
}

/** Puts key first among the person's recent issues. */
export function rememberIssue(userId: string | undefined, key: string): void {
  if (!userId) return;
  const next = [key, ...recentIssueKeys(userId).filter((each) => each !== key)].slice(0, ARMATURE_RECENT_ISSUES);
  try {
    window.localStorage.setItem(ARMATURE_RECENT_STORAGE_PREFIX + userId, JSON.stringify(next));
  } catch {
    // A browser that refuses storage still inserts; it just offers no recent issues.
  }
}
