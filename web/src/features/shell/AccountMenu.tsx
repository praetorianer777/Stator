import { useNavigate } from "@tanstack/react-router";
import { useLogout, useMe } from "@/api/auth";
import { useConnectedTools } from "@/api/tools";
import { Avatar, Button, Menu, type MenuEntry, type MenuItem } from "@/components/ui";
import type { AnchoredSide } from "@/components/ui/overlay";
import { Icon } from "@/components/icons";
import { CONNECTED_TOOLS, SETTINGS_PATH } from "@/config";
import { useCanAdministerSite } from "@/features/permissions/access";
import { t } from "@/i18n";
import { usePlaces } from "./places";

/**
 * The avatar and its menu, "Your account": who is signed in, their own pages,
 * the other tools, Settings for site administrators, and Sign out.
 */
export function AccountMenu({ showName = false, side = "right" }: { showName?: boolean; side?: AnchoredSide }) {
  const navigate = useNavigate();
  const { data: me } = useMe();
  const logout = useLogout();
  const siteAdmin = useCanAdministerSite();
  const places = usePlaces();
  const tools = useConnectedTools(Boolean(me?.organization)).data ?? [];
  const name = me?.user.name ?? t.account.guest;
  const role = me?.organization?.role;

  const items: MenuEntry[] = places
    .filter((place) => place.inAccountMenu)
    .map((place) => {
      const Glyph = Icon[place.icon];
      return { label: place.label, icon: <Glyph />, onSelect: () => void navigate({ to: place.to }), attrs: { "data-action": place.id } };
    });
  // Listed once the organization is connected, whether or not the person
  // connected their own account: single sign-on signs them in over there.
  if (tools.length > 0) {
    items.push({
      group: t.account.switchTo,
      attrs: { "data-switch-to": "" },
      items: tools.map(
        (tool): MenuItem => ({
          label: CONNECTED_TOOLS[tool.product].name,
          icon: <img src={CONNECTED_TOOLS[tool.product].mark} alt="" />,
          href: tool.url,
          attrs: { "data-tool": tool.product },
        }),
      ),
    });
  }
  if (siteAdmin) {
    items.push({
      label: t.account.settings,
      icon: <Icon.Settings />,
      onSelect: () => void navigate({ to: SETTINGS_PATH }),
      attrs: { "data-action": "settings" },
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
    <Menu
      label={t.account.menu}
      side={side}
      header={
        <div className="border-b border-border px-2 pt-1 pb-2" data-account-header>
          <p className="truncate text-sm font-medium text-ink" data-account-header-name>
            {name}
          </p>
          {me?.user.email && <p className="truncate text-xs text-ink-muted">{me.user.email}</p>}
          {role && (
            <p className="text-xs text-ink-subtle" data-account-role={role}>
              {t.sso.roleNames[role] ?? role}
            </p>
          )}
        </div>
      }
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
          {showName && (
            <span className="min-w-0 truncate text-sm text-ink" data-account-name>
              {name}
            </span>
          )}
        </Button>
      )}
      items={items}
    />
  );
}
