import { safeHref, shortcutName, useShortcuts, type Shortcut } from "@/api/shortcuts";
import { Icon } from "@/components/icons";
import { EXTERNAL_LINK_REL } from "@/config";
import { PageLink } from "@/features/pages/PageLink";
import { t } from "@/i18n";

const item = "flex h-8 items-center gap-2 rounded-control px-2 text-sm text-ink-muted no-underline hover:bg-surface-raised hover:text-ink";

/** The space's shortcuts above its page tree, in the order its administrators gave them; nothing while there are none. */
export function SidebarShortcuts({ spaceKey, onNavigate }: { spaceKey: string; onNavigate?: () => void }) {
  const { data: shortcuts } = useShortcuts(spaceKey);
  if (!shortcuts?.length) return null;
  return (
    <ul className="my-1 space-y-px border-b border-border pb-1" aria-label={t.shortcuts.sidebar} data-sidebar-shortcuts={spaceKey}>
      {shortcuts.map((shortcut) => (
        <li key={shortcut.id} data-shortcut={shortcutName(shortcut)}>
          <ShortcutLink shortcut={shortcut} onNavigate={onNavigate} />
        </li>
      ))}
    </ul>
  );
}

function ShortcutLink({ shortcut, onNavigate }: { shortcut: Shortcut; onNavigate?: () => void }) {
  const name = shortcutName(shortcut);
  if (shortcut.page) {
    return (
      <PageLink
        spaceKey={shortcut.page.spaceKey}
        id={shortcut.page.id}
        title={shortcut.page.title}
        home={shortcut.page.home}
        className={item}
        onClick={onNavigate}
      >
        <Icon.Page className="shrink-0" />
        <span className="min-w-0 flex-1 truncate">{name}</span>
      </PageLink>
    );
  }
  const href = safeHref(shortcut.url);
  if (!href) return null;
  return (
    <a href={href} target="_blank" rel={EXTERNAL_LINK_REL} className={item} onClick={onNavigate}>
      <Icon.Link className="shrink-0" />
      <span className="min-w-0 flex-1 truncate">{name}</span>
      <Icon.External className="shrink-0 text-ink-subtle" />
      <span className="sr-only">{t.shortcuts.opensNewTab}</span>
    </a>
  );
}
