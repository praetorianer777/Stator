import { useEffect } from "react";
import { useNavigate } from "@tanstack/react-router";
import { administers, useJoinRequests, useLogout, useMe } from "@/api/auth";
import { Avatar, Button, IconButton, Menu, type MenuItem } from "@/components/ui";
import { Icon } from "@/components/icons";
import { AUDIT_PATH, STALE_PATH } from "@/config";
import { NotificationBell } from "@/features/notifications/NotificationBell";
import { useCanAdministerOrg } from "@/features/permissions/access";
import { t } from "@/i18n";
import { DRAWER_ID } from "./state";

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

/** Above the content: the drawer's button on a narrow screen, quick search, and the person signed in with what they may do. */
export function TopBar({
  narrow,
  drawerOpen,
  onOpenDrawer,
  searching,
  onSearch,
}: {
  narrow: boolean;
  drawerOpen: boolean;
  onOpenDrawer: () => void;
  searching: boolean;
  onSearch: () => void;
}) {
  const navigate = useNavigate();
  const { data: me } = useMe();
  const logout = useLogout();
  const name = me?.user.name ?? t.account.guest;
  const admin = administers(me?.organization?.role);
  const orgAdmin = useCanAdministerOrg();
  const waiting = useJoinRequests(admin).data?.length ?? 0;
  const items: MenuItem[] = [
    { label: t.account.profile, icon: <Icon.User />, onSelect: () => navigate({ to: "/settings/profile" }), attrs: { "data-action": "profile" } },
    { label: t.account.themes, icon: <Icon.Palette />, onSelect: () => navigate({ to: "/settings/themes" }), attrs: { "data-action": "themes" } },
    { label: t.account.tokens, icon: <Icon.Key />, onSelect: () => navigate({ to: "/settings/tokens" }), attrs: { "data-action": "tokens" } },
    {
      label: t.account.notifications,
      icon: <Icon.Bell />,
      onSelect: () => navigate({ to: "/settings/notifications" }),
      attrs: { "data-action": "notification-settings" },
    },
    { label: t.account.watching, icon: <Icon.Eye />, onSelect: () => navigate({ to: "/settings/watching" }), attrs: { "data-action": "watching" } },
  ];
  if (admin) {
    items.push({
      label: (
        <>
          <span className="flex-1">{t.account.sso}</span>
          {waiting > 0 && (
            <span className="rounded-full bg-accent px-1.5 text-2xs font-medium text-on-primary" data-join-badge>
              <span aria-hidden="true">{waiting}</span>
              <span className="sr-only">{t.sso.waitingCount(waiting)}</span>
            </span>
          )}
        </>
      ),
      icon: <Icon.Users />,
      onSelect: () => navigate({ to: "/settings/sso" }),
      attrs: { "data-action": "sso-settings" },
    });
  }
  if (orgAdmin) {
    items.push({
      label: t.account.permissions,
      icon: <Icon.Lock />,
      onSelect: () => navigate({ to: "/settings/permissions" }),
      attrs: { "data-action": "org-permissions" },
    });
    items.push({
      label: t.account.armature,
      icon: <Icon.Link />,
      onSelect: () => navigate({ to: "/settings/armature" }),
      attrs: { "data-action": "armature-settings" },
    });
    items.push({
      label: t.account.audit,
      icon: <Icon.Shield />,
      onSelect: () => navigate({ to: AUDIT_PATH }),
      attrs: { "data-action": "audit-log" },
    });
    items.push({
      label: t.account.stale,
      icon: <Icon.Calendar />,
      onSelect: () => navigate({ to: STALE_PATH }),
      attrs: { "data-action": "stale-pages" },
    });
  }
  items.push({
    label: t.account.signOut,
    icon: <Icon.External />,
    // Wherever the request ends, the session is no longer this browser's to use.
    onSelect: () => logout.mutate(undefined, { onSettled: () => navigate({ to: "/login" }) }),
    disabled: !me,
    attrs: { "data-action": "sign-out" },
  });
  return (
    <header className="flex h-12 shrink-0 items-center gap-2 border-b border-border bg-surface px-2 shell:gap-3 shell:px-4" data-top-bar data-print-hide>
      {narrow && (
        <IconButton
          icon={<Icon.Menu />}
          label={t.nav.openDrawer}
          size="md"
          onClick={onOpenDrawer}
          aria-haspopup="dialog"
          aria-expanded={drawerOpen}
          aria-controls={drawerOpen ? DRAWER_ID : undefined}
          data-action="drawer"
        />
      )}
      <button
        type="button"
        onClick={onSearch}
        aria-haspopup="dialog"
        aria-expanded={searching}
        className="flex h-8 w-full max-w-md items-center gap-2 rounded-control border border-border-strong/70 bg-canvas px-2.5 text-sm text-ink-subtle hover:border-border-strong"
        data-action="search"
      >
        <Icon.Search className="shrink-0" />
        <span className="min-w-0 flex-1 truncate text-left">{t.search.placeholder}</span>
        <kbd className="hidden font-mono text-2xs shell:inline">{t.search.shortcut}</kbd>
      </button>
      <div className="ml-auto flex items-center gap-1">
        {me?.organization && <NotificationBell />}
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
              aria-controls={props["aria-controls"]}
              aria-label={t.account.menu}
              className="px-1!"
              data-action="account"
            >
              <Avatar name={name} src={me?.user.avatarUrl} size="sm" />
              <span className="hidden text-sm text-ink shell:inline" data-account-name>
                {name}
              </span>
            </Button>
          )}
          items={items}
        />
      </div>
    </header>
  );
}
