import { cx } from "@/components/ui";
import { MENU_GAP_PX } from "@/config";
import { t } from "@/i18n";
import type { Mentionable } from "./schema";
import { useFollowActive } from "./useFollowActive";

/**
 * The people an at sign could mean, drawn under the caret. The editor keeps
 * focus and points at the active option with aria-activedescendant.
 */
export function MentionList({
  id,
  items,
  active,
  rect,
  onPick,
  onHover,
}: {
  id: string;
  items: Mentionable[];
  active: number;
  rect: DOMRect | null;
  onPick: (person: Mentionable) => void;
  onHover: (i: number) => void;
}) {
  const follow = useFollowActive(active);
  if (items.length === 0 || !rect) return null;
  return (
    <div
      ref={follow}
      id={id}
      role="listbox"
      aria-label={t.editor.mentions}
      data-mention-list
      className="fixed z-40 max-h-80 min-w-56 overflow-y-hidden rounded-overlay border border-border bg-surface-overlay p-1 shadow-2"
      style={{ left: rect.left, top: rect.bottom + MENU_GAP_PX }}
    >
      {items.map((person, i) => (
        <div
          key={person.id}
          id={`${id}-${i}`}
          role="option"
          aria-selected={i === active}
          tabIndex={-1}
          data-mention-option={person.name}
          data-cannot-view={person.canView === false ? "" : undefined}
          onMouseDown={(e) => {
            e.preventDefault();
            onPick(person);
          }}
          onMouseEnter={() => onHover(i)}
          className={cx(
            "flex cursor-pointer items-baseline gap-2 rounded-control px-2 py-1.5 text-sm",
            i === active ? "bg-surface-raised text-ink" : "text-ink-muted",
          )}
        >
          <span className="font-medium text-ink">{person.name}</span>
          {person.email && <span className="truncate text-xs text-ink-subtle">{person.email}</span>}
          {person.canView === false && <span className="ml-auto shrink-0 text-xs text-ink-muted">{t.editor.mentionCannotView}</span>}
        </div>
      ))}
    </div>
  );
}

/** People whose name or address starts a word with what was typed after the at sign. */
export function mentionMatches(people: Mentionable[], query: string): Mentionable[] {
  const q = query.trim().toLowerCase();
  if (!q) return people;
  return people.filter(
    (p) =>
      p.name
        .toLowerCase()
        .split(/\s+/)
        .some((word) => word.startsWith(q)) || p.email?.toLowerCase().startsWith(q),
  );
}
