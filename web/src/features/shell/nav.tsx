import type { ReactNode } from "react";
import { Link } from "@tanstack/react-router";
import { Tooltip, cx } from "@/components/ui";
import { Icon, type IconName } from "@/components/icons";
import { guestSpaceOf, useMe } from "@/api/auth";
import { t } from "@/i18n";

/** A destination in the rail or the sidebar: an icon with its word, or the icon alone with the word in a tooltip. */
export function NavItem({
  to,
  search,
  exact = false,
  icon,
  rail,
  trailing,
  onNavigate,
  children,
}: {
  to: string;
  /** The query the link carries, such as a tab to open. */
  search?: Record<string, string>;
  exact?: boolean;
  icon: IconName;
  rail: boolean;
  trailing?: ReactNode;
  /** Called when the link is followed, even to the page already open. */
  onNavigate?: () => void;
  children: string;
}) {
  const Glyph = Icon[icon];
  const link = (
    <Link
      to={to}
      search={search}
      onClick={onNavigate}
      activeOptions={{ exact }}
      className={cx(
        "flex h-8 items-center gap-2 rounded-control text-sm text-ink-muted hover:bg-surface-raised hover:text-ink",
        rail ? "w-8 justify-center px-0" : "px-2",
      )}
      activeProps={{ className: "bg-accent-subtle text-accent font-medium hover:bg-accent-subtle hover:text-accent" }}
    >
      <Glyph className="shrink-0" />
      {!rail && <span className="min-w-0 flex-1 truncate">{children}</span>}
      {!rail && trailing}
      {rail && <span className="sr-only">{children}</span>}
    </Link>
  );
  return rail ? (
    <Tooltip text={children} side="right">
      {link}
    </Tooltip>
  ) : (
    link
  );
}

/** The space directory, which a guest goes without: their one space is all they reach. */
export function SpacesNavItem({ rail, onNavigate }: { rail: boolean; onNavigate?: () => void }) {
  const { data: me } = useMe();
  if (guestSpaceOf(me)) return null;
  return (
    <NavItem to="/spaces" icon="Space" rail={rail} onNavigate={onNavigate}>
      {t.nav.spaces}
    </NavItem>
  );
}
