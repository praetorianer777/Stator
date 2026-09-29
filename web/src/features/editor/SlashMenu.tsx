import { useEffect } from "react";
import { cx } from "@/components/ui";
import { MENU_GAP_PX } from "@/config";
import { t } from "@/i18n";
import type { SlashItem } from "./slashItems";

/**
 * The blocks a slash can insert, drawn under the caret. Focus stays in the
 * editor; the active option is announced through aria-activedescendant.
 */
export function SlashMenu({
  id,
  items,
  active,
  rect,
  onPick,
  onHover,
}: {
  id: string;
  items: SlashItem[];
  active: number;
  rect: DOMRect | null;
  onPick: (item: SlashItem) => void;
  onHover: (i: number) => void;
}) {
  useEffect(() => {
    document.getElementById(`${id}-${active}`)?.scrollIntoView?.({ block: "nearest" });
  }, [id, active]);
  if (!rect) return null;
  const frame = "fixed z-40 max-h-80 w-72 overflow-y-auto rounded-overlay border border-border bg-surface-overlay p-1 shadow-2";
  const place = { left: rect.left, top: rect.bottom + MENU_GAP_PX };
  // An empty listbox is no list at all to a screen reader, so the empty
  // state is a status message in its place.
  if (items.length === 0) {
    return (
      <div id={id} className={frame} style={place} data-slash-menu>
        <p role="status" className="px-2 py-1.5 text-sm text-ink-muted" data-slash-empty>
          {t.editor.slashEmpty}
        </p>
      </div>
    );
  }
  return (
    <div id={id} role="listbox" aria-label={t.editor.slashMenu} data-slash-menu className={frame} style={place}>
      {items.map((item, i) => (
        <div
          key={item.key}
          id={`${id}-${i}`}
          role="option"
          aria-selected={i === active}
          tabIndex={-1}
          data-slash-item={item.key}
          onMouseDown={(e) => {
            e.preventDefault();
            onPick(item);
          }}
          onMouseEnter={() => onHover(i)}
          className={cx("flex cursor-pointer items-start gap-2 rounded-control px-2 py-1.5", i === active ? "bg-surface-raised" : "")}
        >
          <span className="mt-0.5 text-ink-muted">
            <item.icon />
          </span>
          <span className="min-w-0">
            <span className="block text-sm font-medium text-ink">{item.label}</span>
            <span className="block text-xs text-ink-muted">{item.description}</span>
          </span>
        </div>
      ))}
    </div>
  );
}
