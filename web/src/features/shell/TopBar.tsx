import { useEffect } from "react";
import { useNavigate } from "@tanstack/react-router";
import { Avatar, Button, Menu } from "@/components/ui";
import { Icon } from "@/components/icons";
import { t } from "@/i18n";

/** Ctrl or Cmd+K anywhere in the shell calls onSearch. */
export function useSearchShortcut(onSearch: () => void) {
  useEffect(() => {
    function onKey(event: KeyboardEvent) {
      if ((event.ctrlKey || event.metaKey) && event.key.toLowerCase() === "k") {
        event.preventDefault();
        onSearch();
      }
    }
    document.addEventListener("keydown", onKey);
    return () => document.removeEventListener("keydown", onKey);
  }, [onSearch]);
}

/**
 * Above the content: the way into search, and the person. There are no
 * accounts yet, so the person is a guest and the menu's entries wait for them.
 */
export function TopBar() {
  const navigate = useNavigate();
  return (
    <header className="flex h-12 shrink-0 items-center gap-3 border-b border-border bg-surface px-4" data-top-bar data-print-hide>
      <button
        type="button"
        onClick={() => navigate({ to: "/search" })}
        className="flex h-8 w-full max-w-md items-center gap-2 rounded-control border border-border-strong/70 bg-canvas px-2.5 text-sm text-ink-subtle hover:border-border-strong"
        data-action="search"
      >
        <Icon.Search className="shrink-0" />
        <span className="min-w-0 flex-1 truncate text-left">{t.search.placeholder}</span>
        <kbd className="font-mono text-2xs">{t.search.shortcut}</kbd>
      </button>
      <div className="ml-auto flex items-center">
        <Menu
          label={t.account.menu}
          align="end"
          trigger={(props) => (
            <Button
              variant="ghost"
              size="sm"
              onClick={props.toggle}
              aria-haspopup={props["aria-haspopup"]}
              aria-expanded={props["aria-expanded"]}
              aria-label={t.account.menu}
              className="px-1!"
              data-action="account"
            >
              <Avatar name={t.account.guest} size="sm" />
              <span className="text-sm text-ink">{t.account.guest}</span>
            </Button>
          )}
          items={[
            { label: t.account.profile, icon: <Icon.User />, onSelect: () => {}, disabled: true },
            { label: t.account.signOut, icon: <Icon.External />, onSelect: () => {}, disabled: true },
          ]}
        />
      </div>
    </header>
  );
}
