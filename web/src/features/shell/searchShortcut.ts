import { useEffect } from "react";

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
