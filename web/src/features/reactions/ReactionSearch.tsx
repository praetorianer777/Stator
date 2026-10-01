import { useEffect, useId, useState } from "react";
import { Dialog, Input } from "@/components/ui";
import { REACTION_SEARCH_LIMIT } from "@/config";
import { t } from "@/i18n";
import { loadEmoji, matchEmoji, type Emoji } from "@/lib/emoji";

/**
 * Every emoji the editor's colon offers, found by name, for a reaction the
 * quick picker does not hold. Enter in the box takes the first one found.
 */
export function ReactionSearch({ onPick, onClose }: { onPick: (emoji: string) => void; onClose: () => void }) {
  const [all, setAll] = useState<Emoji[] | null>(null);
  const [failed, setFailed] = useState(false);
  const [query, setQuery] = useState("");
  const listId = useId();
  useEffect(() => {
    let live = true;
    loadEmoji().then(
      (loaded) => live && setAll(loaded),
      () => live && setFailed(true),
    );
    return () => {
      live = false;
    };
  }, []);
  const found = all ? matchEmoji(all, query, REACTION_SEARCH_LIMIT) : [];
  const pick = (emoji: string) => {
    onPick(emoji);
    onClose();
  };
  return (
    <Dialog title={t.reactions.searchTitle} onClose={onClose} data-reaction-search="">
      <Input
        type="search"
        aria-label={t.reactions.searchLabel}
        aria-controls={listId}
        placeholder={t.reactions.searchPlaceholder}
        value={query}
        onChange={(e) => setQuery(e.target.value)}
        onKeyDown={(e) => {
          if (e.key === "Enter" && found[0]) {
            e.preventDefault();
            pick(found[0].emoji);
          }
        }}
        data-reaction-search-input=""
      />
      {failed ? (
        <p role="alert" className="mt-3 text-sm text-danger">
          {t.reactions.searchFailed}
        </p>
      ) : !all ? (
        <p className="mt-3 text-sm text-ink-muted">{t.reactions.searchLoading}</p>
      ) : found.length === 0 ? (
        <p role="status" className="mt-3 text-sm text-ink-muted" data-reaction-search-empty="">
          {t.reactions.searchEmpty}
        </p>
      ) : (
        <ul id={listId} aria-label={t.reactions.searchResults} className="mt-3 grid grid-cols-6 gap-1 sm:grid-cols-8">
          {found.map((item) => (
            <li key={item.emoji}>
              <button
                type="button"
                aria-label={item.description}
                title={item.description}
                onClick={() => pick(item.emoji)}
                className="flex size-9 items-center justify-center rounded-control text-xl hover:bg-surface-raised"
                data-reaction-search-option={item.emoji}
              >
                <span aria-hidden="true">{item.emoji}</span>
              </button>
            </li>
          ))}
        </ul>
      )}
    </Dialog>
  );
}
