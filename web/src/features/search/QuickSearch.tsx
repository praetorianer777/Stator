import { useEffect, useId, useRef, useState, type KeyboardEvent, type ReactNode } from "react";
import { createPortal } from "react-dom";
import { useNavigate } from "@tanstack/react-router";
import { useLogout, useMe } from "@/api/auth";
import { useMarkRead } from "@/api/notifications";
import { useQuickSearch, useRecentPages, type PageHit } from "@/api/search";
import { useSpaces } from "@/api/spaces";
import { cx } from "@/components/ui";
import { useEscape, useFocusReturn } from "@/components/ui/overlay";
import { Icon } from "@/components/icons";
import { QUICK_SEARCH_DEBOUNCE_MS, SEARCH_QUERY_MAX_LENGTH } from "@/config";
import { t } from "@/i18n";
import { useFollowActive } from "@/features/editor/useFollowActive";
import { useCanCreateSpace } from "@/features/permissions/access";
import { adminEntry, usePlaces, type Place } from "@/features/shell/places";
import { SETTINGS_PATH } from "@/config";
import { useDebounced } from "@/lib/debounce";
import { pageSlug } from "@/lib/slug";

/** No option is active: the query itself is, and Enter searches everything for it. */
const ON_QUERY = -1;

/** One thing the palette offers: where it leads or what it does, and how it is drawn. */
interface Option {
  key: string;
  icon: ReactNode;
  label: string;
  detail?: string;
  run: () => void;
  attrs: Record<string, string>;
}

/** Options under one heading, read out as a list of their own. */
interface Group {
  id: "places" | "spaces" | "actions" | "found" | "recent";
  heading: string;
  options: Option[];
}

/**
 * The Ctrl K palette, Cmd K on a Mac: the places of the sidebar, the avatar
 * menu and the settings area, the spaces and what can be done, all as the
 * reader types, page titles as they type and recent pages while nothing is,
 * and Enter on the words for the full search. Each kind is a group of its own.
 */
export function QuickSearch({ onClose }: { onClose: () => void }) {
  const navigate = useNavigate();
  const ref = useRef<HTMLDivElement>(null);
  const listId = useId();
  const reachable = useReachablePlaces();
  const actions = useActions(onClose);
  const spaces = useSpaces();
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

  const go = (to: string, hash?: string) => {
    onClose();
    void navigate({ to, hash });
  };
  const groups: Group[] = [
    {
      id: "places",
      heading: t.search.places,
      options: (wanted ? reachable.filter((place) => matches(place.label, wanted)) : []).map((place) => {
        const Glyph = Icon[place.icon];
        return {
          key: place.id,
          icon: <Glyph />,
          label: place.label,
          detail: sectionOf(place),
          run: () => go(place.to, place.hash),
          attrs: { "data-quick-search-place": place.id },
        };
      }),
    },
    {
      id: "spaces",
      heading: t.search.spaces,
      options: (wanted ? (spaces.data ?? []).filter((space) => matches(space.name, wanted) || space.key.toLowerCase().startsWith(wanted)) : []).map(
        (space) => ({
          key: space.key,
          icon: <Icon.Space />,
          label: space.name,
          detail: space.key,
          run: () => go(`/s/${space.key}`),
          attrs: { "data-quick-search-space": space.key },
        }),
      ),
    },
    { id: "actions", heading: t.search.actions, options: wanted ? actions.filter((action) => matches(action.label, wanted)) : [] },
    {
      id: typed ? "found" : "recent",
      heading: typed ? t.search.found : t.search.recent,
      options: pages.map((page) => ({
        key: page.id,
        icon: <Icon.Page />,
        label: page.title,
        detail: [page.spaceName, ...page.path.slice(1)].join(" / "),
        run: () => {
          onClose();
          void navigate({ to: "/s/$spaceKey/p/$pageId/$slug", params: { spaceKey: page.spaceKey, pageId: page.id, slug: pageSlug(page.title) } });
        },
        attrs: { "data-quick-search-option": page.title },
      })),
    },
  ];
  const shown = groups.filter((group) => group.options.length > 0);
  // The arrows go through the groups in the order they are drawn.
  const options = shown.flatMap((group) => group.options);
  const count = options.length;
  const current = active < count ? active : ON_QUERY;
  const optionId = (index: number) => `${listId}-${index}`;
  const groupId = (group: Group) => `${listId}-${group.id}`;
  // The wheel and a swipe still scroll the clipped list by hand; the active
  // option sits in a group, so it is found by its id rather than by its place.
  const follow = useFollowActive(ON_QUERY);
  useEffect(() => {
    if (current !== ON_QUERY) document.getElementById(`${listId}-${current}`)?.scrollIntoView?.({ block: "nearest" });
  }, [listId, current]);

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
      const option = current === ON_QUERY ? undefined : options[current];
      if (option) option.run();
      else searchEverything();
    }
  }

  const empty = settled && count === 0 && (!typed || words !== "");
  const controls = shown.map(groupId).join(" ");
  const said: Record<Group["id"], (n: number) => string> = {
    places: t.search.placesCount,
    spaces: t.search.spacesCount,
    actions: t.search.actionsCount,
    found: t.search.optionsCount,
    recent: t.search.optionsCount,
  };
  let index = 0;
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
        <div ref={follow} className="max-h-[60vh] overflow-y-hidden p-1">
          {shown.map((group) => (
            <div key={group.id}>
              <p id={`${groupId(group)}-heading`} className="px-2 pt-2 pb-1 text-2xs font-medium tracking-wide text-ink-subtle uppercase">
                {group.heading}
              </p>
              <div id={groupId(group)} role="listbox" aria-labelledby={`${groupId(group)}-heading`} data-quick-search-list={group.id}>
                {group.options.map((option) => {
                  const at = index++;
                  return (
                    <div
                      key={option.key}
                      id={optionId(at)}
                      role="option"
                      aria-selected={at === current}
                      tabIndex={-1}
                      onMouseMove={() => at !== current && setActive(at)}
                      // Picked on press, so focus never leaves the input that names the active option.
                      onMouseDown={(e) => {
                        e.preventDefault();
                        option.run();
                      }}
                      className={cx("flex cursor-pointer items-center gap-2 rounded-control px-2 py-1.5 text-sm", at === current && "bg-accent-subtle")}
                      {...option.attrs}
                    >
                      <span className="shrink-0 text-ink-muted [&_svg]:size-4">{option.icon}</span>
                      <span className="min-w-0 flex-1 truncate text-ink">{option.label}</span>
                      {option.detail && <span className="max-w-[45%] shrink-0 truncate text-2xs text-ink-muted">{option.detail}</span>}
                    </div>
                  );
                })}
              </div>
            </div>
          ))}
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
          {settled && !failed
            ? [
                ...shown.filter((group) => group.id !== "found" && group.id !== "recent").map((group) => said[group.id](group.options.length)),
                t.search.optionsCount(pages.length),
              ].join(", ")
            : ""}
        </p>
      </div>
    </div>,
    document.body,
  );
}

/** What can be done from anywhere, for whoever may do it. */
function useActions(onClose: () => void): Option[] {
  const navigate = useNavigate();
  const { data: me } = useMe();
  const mayCreate = useCanCreateSpace();
  const markRead = useMarkRead();
  const logout = useLogout();
  const act = (key: string, icon: ReactNode, label: string, run: () => void): Option => ({
    key,
    icon,
    label,
    run: () => {
      onClose();
      run();
    },
    attrs: { "data-quick-search-action": key },
  });
  return [
    ...(mayCreate
      ? [
          act("new-space", <Icon.Plus />, t.spaces.create, () => void navigate({ to: "/spaces/new" })),
          act("import-space", <Icon.Upload />, t.spaceTransfer.importSpace, () => void navigate({ to: "/spaces/import" })),
        ]
      : []),
    ...(me?.organization ? [act("mark-all-read", <Icon.Check />, t.notifications.markAllRead, () => markRead.mutate({ all: true }))] : []),
    act("sign-out", <Icon.External />, t.account.signOut, () => logout.mutate(undefined, { onSettled: () => void navigate({ to: "/login" }) })),
  ];
}

/** Every place the reader may go, the settings hub, and for an administrator Admin with each of its pages as "Admin / <page>". */
function useReachablePlaces(): Place[] {
  const places = usePlaces();
  const settings: Place = { id: "settings", area: "you", to: SETTINGS_PATH, icon: "Settings", label: t.settings.title, exact: true };
  const named = places.map((place) => (place.area === "admin" ? { ...place, label: t.admin.inPalette(place.label) } : place));
  return places.some((place) => place.area === "admin") ? [...named, settings, adminEntry()] : [...named, settings];
}

function sectionOf(place: Place): string {
  return { wiki: t.nav.groupWiki, you: t.settings.you, admin: t.admin.title }[place.area];
}

// The start of the label or of one of its words, as the page titles are found by their start.
function matches(label: string, wanted: string): boolean {
  const lower = label.toLowerCase();
  return lower.startsWith(wanted) || lower.split(/\s+/).some((word) => word.startsWith(wanted));
}
