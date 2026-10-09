import { cx } from "@/components/ui";
import { CARET_MENU_MIN_WIDTH_PX } from "@/config";
import { CARET_MENU_HEIGHT_CLASS, caretMenuPlaceNow } from "./caretMenu";
import { t } from "@/i18n";
import type { Emoji } from "./emoji";
import { useFollowActive } from "./useFollowActive";

/**
 * The emoji a colon and the letters after it could mean, drawn under the
 * caret. The editor keeps focus and points at the active option with
 * aria-activedescendant.
 */
export function EmojiList({
  id,
  items,
  active,
  rect,
  onPick,
  onHover,
}: {
  id: string;
  items: Emoji[];
  active: number;
  rect: DOMRect | null;
  onPick: (emoji: Emoji) => void;
  onHover: (i: number) => void;
}) {
  const follow = useFollowActive(active);
  if (!rect) return null;
  const frame = `fixed z-40 min-w-56 ${CARET_MENU_HEIGHT_CLASS} overflow-y-hidden rounded-overlay border border-border bg-surface-overlay p-1 shadow-2`;
  const place = caretMenuPlaceNow(rect, CARET_MENU_MIN_WIDTH_PX);
  // An empty listbox is no list at all to a screen reader, so the empty
  // state is a status message in its place.
  if (items.length === 0) {
    return (
      <div id={id} className={frame} style={place} data-emoji-list>
        <p role="status" className="px-2 py-1.5 text-sm text-ink-muted" data-emoji-empty>
          {t.inlineValues.emoji.empty}
        </p>
      </div>
    );
  }
  return (
    <div ref={follow} id={id} role="listbox" aria-label={t.inlineValues.emoji.list} data-emoji-list className={frame} style={place}>
      {items.map((item, i) => (
        <div
          key={item.emoji}
          id={`${id}-${i}`}
          role="option"
          aria-selected={i === active}
          tabIndex={-1}
          data-emoji-option={item.names[0]}
          onMouseDown={(e) => {
            e.preventDefault();
            onPick(item);
          }}
          onMouseEnter={() => onHover(i)}
          className={cx(
            "flex cursor-pointer items-center gap-2 rounded-control px-2 py-1 text-sm",
            i === active ? "bg-surface-raised text-ink" : "text-ink-muted",
          )}
        >
          <span aria-hidden="true" className="text-base leading-none">
            {item.emoji}
          </span>
          <span className="font-medium text-ink">:{item.names[0]}:</span>
          <span className="truncate text-xs text-ink-muted">{item.description}</span>
        </div>
      ))}
    </div>
  );
}
