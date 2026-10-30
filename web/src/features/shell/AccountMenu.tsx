import { useNavigate } from "@tanstack/react-router";
import { useLogout, useMe } from "@/api/auth";
import { useConnectedTools } from "@/api/tools";
import { Avatar, IconButton, Menu, type MenuEntry, type MenuItem } from "@/components/ui";
import type { AnchoredSide } from "@/components/ui/overlay";
import { Icon } from "@/components/icons";
import { CONNECTED_TOOLS } from "@/config";
import { useCanAdministerSite } from "@/features/permissions/access";
import { t } from "@/i18n";
import { adminEntry, usePlaces } from "./places";

/**
 * The round avatar and its menu, "Your account": who is signed in, their own
 * pages, the other tools, Admin for administrators, and Sign out.
 */
export function AccountMenu({ side = "right" }: { side?: AnchoredSide }) {
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
    const admin = adminEntry();
    items.push({
      label: admin.label,
      icon: <Icon.Settings />,
      separated: true,
      onSelect: () => void navigate({ to: admin.to }),
      attrs: { "data-action": "admin" },
    });
  }
  items.push({
    label: t.account.signOut,
    icon: <Icon.External />,
    separated: true,
    // Wherever the request ends, the session is no longer this browser's to use.
    onSelect: () => logout.mutate(undefined, { onSettled: () => navigate({ to: "/login" }) }),
    disabled: !me,
    attrs: { "data-action": "sign-out" },
  });

  return (
    <Menu
      label={t.account.menu}
      side={side}
      // As wide as the rail, so the list opens past the rail's edge rather than over it.
      className={side === "right" ? "w-full justify-center" : undefined}
      header={
        <div className="mb-1 flex items-center gap-2 border-b border-border px-2 pt-1 pb-2" data-account-header>
          <Avatar name={name} src={me?.user.avatarUrl} size="md" />
          <span className="min-w-0 flex-1">
            <span className="block truncate text-sm font-medium text-ink" data-account-header-name>
              {name}
            </span>
            {me?.user.email && <span className="block truncate text-xs text-ink-muted">{me.user.email}</span>}
            {role && (
              <span className="block text-2xs text-ink-subtle" data-account-role={role}>
                {t.sso.roleNames[role] ?? role}
              </span>
            )}
          </span>
        </div>
      }
      trigger={(props) => (
        <IconButton
          icon={<Avatar name={name} src={me?.user.avatarUrl} size="sm" />}
          label={t.account.menu}
          size="sm"
          onClick={props.toggle}
          aria-haspopup={props["aria-haspopup"]}
          aria-expanded={props["aria-expanded"]}
          aria-controls={props["aria-controls"]}
          className="rounded-full"
          data-action="account"
        />
      )}
      items={items}
    />
  );
}
