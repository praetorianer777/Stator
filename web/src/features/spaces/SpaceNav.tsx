import { useEffect, useState } from "react";
import { useNavigate, useParams } from "@tanstack/react-router";
import { guestSpaceOf, useMe } from "@/api/auth";
import { usePage, type Page } from "@/api/pages";
import { useSpace, useSpaces, type Space } from "@/api/spaces";
import type { TreeNode } from "@/api/tree";
import { Button, Input, Menu, type MenuItem } from "@/components/ui";
import { Icon } from "@/components/icons";
import { LAST_SPACE_KEY } from "@/config";
import { PageTree } from "@/features/pages/PageTree";
import { PlaceDialog } from "@/features/pages/PlaceDialog";
import { NavItem } from "@/features/shell/nav";
import { SidebarGroup } from "@/features/shell/SidebarGroup";
import { SidebarShortcuts } from "@/features/shortcuts/SidebarShortcuts";
import { t } from "@/i18n";
import { pageSlug } from "@/lib/slug";

function readLastSpace(): string {
  try {
    return window.localStorage.getItem(LAST_SPACE_KEY) ?? "";
  } catch {
    return "";
  }
}

function writeLastSpace(key: string) {
  try {
    window.localStorage.setItem(LAST_SPACE_KEY, key);
  } catch {
    // A browser that refuses storage shows the space only while it is open.
  }
}

/**
 * The space the route names, or the one visited last when it names none, so
 * the sidebar keeps the reader's place on the pages between spaces.
 */
function useShownSpace(): string {
  const { spaceKey } = useParams({ strict: false });
  const [last, setLast] = useState(readLastSpace);
  useEffect(() => {
    if (!spaceKey) return;
    writeLastSpace(spaceKey);
    setLast(spaceKey);
  }, [spaceKey]);
  return spaceKey ?? last;
}

/** The space in the sidebar: a switcher, its home, shortcuts, pages, blog and decisions, then the folded Space admin group. */
export function SpaceNav({
  open,
  onToggle,
  adminOpen,
  onToggleAdmin,
  onNavigate,
}: {
  open: boolean;
  onToggle: () => void;
  adminOpen: boolean;
  onToggleAdmin: () => void;
  onNavigate?: () => void;
}) {
  const { pageId } = useParams({ strict: false });
  const shown = useShownSpace();
  const { data: space } = useSpace(shown);
  const { data: reading } = usePage(pageId);
  const [moving, setMoving] = useState<TreeNode>();
  const navigate = useNavigate();
  const openPath = reading ? reading.page.ancestors.filter((each) => !each.home).map((each) => each.id) : [];

  const arrived = (page: Page) => {
    setMoving(undefined);
    void navigate({ to: "/s/$spaceKey/p/$pageId/$slug", params: { spaceKey: page.spaceKey, pageId: page.id, slug: pageSlug(page.title) } });
  };

  return (
    <SidebarGroup id="space" title={space?.name ?? t.nav.spaces} open={open} onToggle={onToggle}>
      <SpaceSwitcher current={space} onNavigate={onNavigate} />
      {space && (
        <div data-space-nav={space.key}>
          <NavItem to={`/s/${space.key}`} exact icon="Home" rail={false} onNavigate={onNavigate}>
            {t.space.home}
          </NavItem>
          <SidebarShortcuts spaceKey={space.key} onNavigate={onNavigate} />
          <div className="my-1">
            <PageTree space={space} currentId={pageId} openPath={openPath} onMove={setMoving} />
          </div>
          <NavItem to={`/s/${space.key}/blog`} icon="Megaphone" rail={false} onNavigate={onNavigate}>
            {t.space.blog}
          </NavItem>
          <NavItem to={`/s/${space.key}/decisions`} icon="Decision" rail={false} onNavigate={onNavigate}>
            {t.space.decisions}
          </NavItem>
          <SidebarGroup id="space-admin" title={t.space.admin} open={adminOpen} onToggle={onToggleAdmin} nested>
            <NavItem id="space-settings" to={`/s/${space.key}/settings`} exact icon="Settings" rail={false} onNavigate={onNavigate}>
              {t.space.settings}
            </NavItem>
            {space.can.deletePages && (
              <NavItem id="space-trash" to={`/s/${space.key}/settings`} search={{ tab: "trash" }} icon="Trash" rail={false} onNavigate={onNavigate}>
                {t.space.trash}
              </NavItem>
            )}
            <NavItem id="space-archive" to={`/s/${space.key}/settings`} search={{ tab: "archive" }} icon="Archive" rail={false} onNavigate={onNavigate}>
              {t.space.archive}
            </NavItem>
            {space.can.administer && (
              <NavItem id="space-stale" to={`/s/${space.key}/settings/stale`} icon="Calendar" rail={false} onNavigate={onNavigate}>
                {t.space.stale}
              </NavItem>
            )}
          </SidebarGroup>
        </div>
      )}
      {moving && space && (
        <PlaceDialog page={{ id: moving.id, title: moving.title, spaceKey: space.key }} mode="move" onClose={() => setMoving(undefined)} onDone={arrived} />
      )}
    </SidebarGroup>
  );
}

/** Every space the reader may see, narrowed by a filter, and the way to the directory. */
function SpaceSwitcher({ current, onNavigate }: { current: Space | undefined; onNavigate?: () => void }) {
  const [filter, setFilter] = useState("");
  const { data: spaces } = useSpaces();
  const { data: me } = useMe();
  const navigate = useNavigate();
  const wanted = filter.trim().toLowerCase();
  const go = (to: string) => {
    setFilter("");
    onNavigate?.();
    void navigate({ to });
  };
  const items: MenuItem[] = (spaces ?? [])
    .filter((each) => !wanted || each.name.toLowerCase().includes(wanted) || each.key.toLowerCase().includes(wanted))
    .map((each) => ({
      label: (
        <>
          <span className="min-w-0 flex-1 truncate">{each.name}</span>
          <span className="font-mono text-2xs text-ink-subtle">{each.key}</span>
        </>
      ),
      onSelect: () => go(`/s/${each.key}`),
      attrs: { "data-space-option": each.key },
    }));
  if (!guestSpaceOf(me)) {
    items.push({ label: t.space.allSpaces, icon: <Icon.Space />, onSelect: () => go("/spaces"), attrs: { "data-action": "all-spaces" } });
  }
  return (
    <Menu
      label={t.space.switcher}
      className="mb-1 w-full"
      trigger={(props) => (
        <Button
          variant="secondary"
          size="sm"
          onClick={props.toggle}
          aria-haspopup={props["aria-haspopup"]}
          aria-expanded={props["aria-expanded"]}
          aria-controls={props["aria-controls"]}
          aria-label={current ? `${t.space.switcher}: ${current.name}` : t.space.switcher}
          className="w-full justify-start gap-2 px-2! text-left"
          iconRight={<Icon.ChevronDown className="shrink-0 text-ink-subtle" />}
          data-space-switcher={current?.key ?? ""}
        >
          <span className="min-w-0 flex-1 truncate font-medium text-ink">{current?.name ?? t.space.chooseSpace}</span>
          {current && <span className="font-mono text-2xs font-normal text-ink-subtle">{current.key}</span>}
        </Button>
      )}
      header={
        <div className="px-1 pb-1">
          <Input
            aria-label={t.space.filterSpaces}
            value={filter}
            onChange={(e) => setFilter(e.target.value)}
            controlSize="sm"
            placeholder={t.space.filterSpaces}
          />
        </div>
      }
      items={items}
    />
  );
}
