import { useId, useRef, useState, type KeyboardEvent } from "react";
import { createPortal } from "react-dom";
import { useNavigate } from "@tanstack/react-router";
import { useQuickSearch, useRecentPages, type PageHit } from "@/api/search";
import { cx } from "@/components/ui";
import { useEscape, useFocusReturn } from "@/components/ui/overlay";
import { Icon } from "@/components/icons";
import { QUICK_SEARCH_DEBOUNCE_MS, SEARCH_QUERY_MAX_LENGTH } from "@/config";
import { t } from "@/i18n";
import { useFollowActive } from "@/features/editor/useFollowActive";
import { useCanAdministerSite } from "@/features/permissions/access";
import { usePlaces, type Place } from "@/features/shell/places";
import { SETTINGS_PATH } from "@/config";
import { useDebounced } from "@/lib/debounce";
import { pageSlug } from "@/lib/slug";

/** No option is active: the query itself is, and Enter searches everything for it. */
const ON_QUERY = -1;

/**
 * The Ctrl K palette: the places of the sidebar, the avatar menu and the settings
 * area and page titles as the reader types, recent pages while nothing is, and
 * Enter on the words for the full search.
 */
export function QuickSearch({ onClose }: { onClose: () => void }) {
  const navigate = useNavigate();
  const ref = useRef<HTMLDivElement>(null);
  const listId = useId();
  const headingId = useId();
  const placesId = useId();
  const placesHeadingId = useId();
  const reachable = useReachablePlaces();
  const [text, setText] = useState("");
  const [active, setActive] = useState(ON_QUERY);
  useEscape(true, onClose);
  useFocusReturn(true, ref, true);

  const typed = text.trim();
  const words = useDebounced(typed, QUICK_SEARCH_DEBOUNCE_MS);
  const quick = useQuickSearch(typed ? words : "");
  const recent = useRecentPages(!typed);
  const pages: PageHit[] = typed ? (words ? (quick.data ?? []) : []) : (recent.data ?? []);
  const failed = typed ? quick.isError : recent.isError;
  const settled = typed ? words === typed && !quick.isFetching : !recent.isLoading;
  const wanted = typed.toLowerCase();
  const places = wanted ? reachable.filter((place) => matches(place.label, wanted)) : [];
  // Places come first in the arrow order, then pages.
  const count = places.length + pages.length;
  const current = active < count ? active : ON_QUERY;
  const optionId = (index: number) => `${listId}-${index}`;
  const follow = useFollowActive(current);

  const open = (page: PageHit) => {
    onClose();
    void navigate({ to: "/s/$spaceKey/p/$pageId/$slug", params: { spaceKey: page.spaceKey, pageId: page.id, slug: pageSlug(page.title) } });
  };
  const go = (place: Place) => {
    onClose();
    void navigate({ to: place.to, hash: place.hash });
  };
  const searchEverything = () => {
    onClose();
    void navigate({ to: "/search", search: typed ? { q: typed } : {} });
  };

  function onKeyDown(event: KeyboardEvent<HTMLInputElement>) {
    if (event.key === "ArrowDown") {
      event.preventDefault();
      setActive(Math.min(current + 1, count - 1));
    } else if (event.key === "ArrowUp") {
      event.preventDefault();
      setActive(Math.max(current - 1, ON_QUERY));
    } else if (event.key === "Enter") {
      event.preventDefault();
      const place = places[current];
      const page = pages[current - places.length];
      if (place) go(place);
      else if (current !== ON_QUERY && page) open(page);
      else searchEverything();
    }
  }

  const empty = settled && count === 0 && (!typed || words !== "");
  const controls = [places.length > 0 && placesId, pages.length > 0 && listId].filter(Boolean).join(" ");
  return createPortal(
    // biome-ignore lint/a11y: the scrim is for the pointer; the keyboard closes the palette with Escape
    <div className="fixed inset-0 z-50 flex items-start justify-center bg-ink/30 p-4 pt-[10vh]" onClick={(e) => e.target === e.currentTarget && onClose()}>
      <div
        ref={ref}
        role="dialog"
        aria-modal="true"
        aria-label={t.search.quickTitle}
        className="w-full max-w-xl overflow-hidden rounded-overlay border border-border bg-surface-overlay shadow-2"
        data-quick-search
      >
        <div className="flex items-center gap-2 border-b border-border px-3">
          <Icon.Search className="shrink-0 text-ink-subtle" />
          <input
            value={text}
            onChange={(e) => {
              setText(e.target.value);
              setActive(ON_QUERY);
            }}
            onKeyDown={onKeyDown}
            role="combobox"
            aria-label={t.search.quickLabel}
            aria-autocomplete="list"
            aria-expanded={count > 0}
            aria-controls={controls || undefined}
            aria-activedescendant={current === ON_QUERY ? undefined : optionId(current)}
            placeholder={t.search.quickPlaceholder}
            maxLength={SEARCH_QUERY_MAX_LENGTH}
            autoComplete="off"
            spellCheck={false}
            className="h-11 min-w-0 flex-1 bg-transparent text-sm text-ink placeholder:text-ink-subtle focus:outline-none"
            data-quick-search-input
          />
          <kbd className="hidden font-mono text-2xs text-ink-subtle shell:inline">{t.search.escape}</kbd>
        </div>
        <div className="p-1">
          {places.length > 0 && (
            <>
              <p id={placesHeadingId} className="px-2 pt-2 pb-1 text-2xs font-medium tracking-wide text-ink-subtle uppercase">
                {t.search.places}
              </p>
              <div id={placesId} role="listbox" aria-labelledby={placesHeadingId} data-quick-search-list="places">
                {places.map((place, index) => {
                  const Glyph = Icon[place.icon];
                  return (
                    <div
                      key={place.id}
                      id={optionId(index)}
                      role="option"
                      aria-selected={index === current}
                      tabIndex={-1}
                      onMouseMove={() => index !== current && setActive(index)}
                      onMouseDown={(e) => {
                        e.preventDefault();
                        go(place);
                      }}
                      className={cx("flex cursor-pointer items-center gap-2 rounded-control px-2 py-1.5 text-sm", index === current && "bg-accent-subtle")}
                      data-quick-search-place={place.id}
                    >
                      <Glyph className="shrink-0 text-ink-muted" />
                      <span className="min-w-0 flex-1 truncate text-ink">{place.label}</span>
                      <span className="shrink-0 text-2xs text-ink-muted">{sectionOf(place)}</span>
                    </div>
                  );
                })}
              </div>
            </>
          )}
          {pages.length > 0 && (
            <>
              <p id={headingId} className="px-2 pt-2 pb-1 text-2xs font-medium tracking-wide text-ink-subtle uppercase">
                {typed ? t.search.found : t.search.recent}
              </p>
              <div
                ref={follow}
                id={listId}
                role="listbox"
                aria-labelledby={headingId}
                className="max-h-[50vh] overflow-y-hidden"
                data-quick-search-list={typed ? "found" : "recent"}
              >
                {pages.map((page, at) => {
                  const index = places.length + at;
                  return (
                    <div
                      key={page.id}
                      id={optionId(index)}
                      role="option"
                      aria-selected={index === current}
                      tabIndex={-1}
                      onMouseMove={() => index !== current && setActive(index)}
                      // Picked on press, so focus never leaves the input that names the active option.
                      onMouseDown={(e) => {
                        e.preventDefault();
                        open(page);
                      }}
                      className={cx("flex cursor-pointer items-center gap-2 rounded-control px-2 py-1.5 text-sm", index === current && "bg-accent-subtle")}
                      data-quick-search-option={page.title}
                    >
                      <Icon.Page className="shrink-0 text-ink-muted" />
                      <span className="min-w-0 flex-1 truncate text-ink">{page.title}</span>
                      <span className="max-w-[45%] shrink-0 truncate text-2xs text-ink-muted">{[page.spaceName, ...page.path.slice(1)].join(" / ")}</span>
                    </div>
                  );
                })}
              </div>
            </>
          )}
          {failed ? (
            <p className="px-3 py-6 text-center text-sm text-danger" data-quick-search-error>
              {t.search.quickFailed}
            </p>
          ) : (
            empty && (
              <p className="px-3 py-6 text-center text-sm text-ink-subtle" data-quick-search-empty>
                {typed ? t.search.noMatch : t.search.noRecent}
              </p>
            )
          )}
        </div>
        <p className="border-t border-border px-3 py-1.5 text-2xs text-ink-subtle" data-quick-search-hint>
          {t.search.searchEverything(typed)}
        </p>
        <p role="status" className="sr-only">
          {settled && !failed ? [places.length > 0 && t.search.placesCount(places.length), t.search.optionsCount(pages.length)].filter(Boolean).join(", ") : ""}
        </p>
      </div>
    </div>,
    document.body,
  );
}

/** Every place the reader may go, and the settings area itself for those who see it in the avatar menu. */
function useReachablePlaces(): Place[] {
  const places = usePlaces();
  const siteAdmin = useCanAdministerSite();
  return siteAdmin ? [...places, { id: "settings", area: "settings", to: SETTINGS_PATH, icon: "Settings", label: t.account.settings, exact: true }] : places;
}

function sectionOf(place: Place): string {
  return { wiki: t.nav.groupWiki, you: t.settings.you, settings: t.settings.site }[place.area];
}

// The start of the label or of one of its words, as the page titles are found by their start.
function matches(label: string, wanted: string): boolean {
  const lower = label.toLowerCase();
  return lower.startsWith(wanted) || lower.split(/\s+/).some((word) => word.startsWith(wanted));
}
