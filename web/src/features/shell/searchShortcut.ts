import { useEffect } from "react";
import { t } from "@/i18n";

// Ctrl or Cmd+K anywhere in the shell, fields included, as in Armature: it is
// the product's one shortcut and a field has no other use for the key.
export function useSearchShortcut(onSearch: () => void) {
  useEffect(() => {
    function onKey(event: KeyboardEvent) {
      if ((event.ctrlKey || event.metaKey) && !event.repeat && event.key.toLowerCase() === "k") {
        event.preventDefault();
        onSearch();
      }
    }
    document.addEventListener("keydown", onKey);
    return () => document.removeEventListener("keydown", onKey);
  }, [onSearch]);
}

/** Whether the reader is on a Mac, whose keyboard says Cmd where others say Ctrl. */
export function onMac(): boolean {
  if (typeof navigator === "undefined") return false;
  const platform = (navigator as Navigator & { userAgentData?: { platform?: string } }).userAgentData?.platform ?? navigator.platform ?? "";
  return /mac|iphone|ipad/i.test(platform);
}

/** The palette's shortcut as this reader's keyboard names it, in their language. */
export function searchShortcutHint(): string {
  return onMac() ? t.search.shortcutMac : t.search.shortcut;
}
