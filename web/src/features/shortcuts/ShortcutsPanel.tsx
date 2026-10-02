import { useEffect, useRef, useState, type FormEvent } from "react";
import { ApiError } from "@/api/client";
import { safeHref, shortcutName, useAddShortcut, useMoveShortcut, useRemoveShortcut, useShortcuts, type Shortcut } from "@/api/shortcuts";
import { useSpaces, type Space } from "@/api/spaces";
import { useOutline } from "@/api/tree";
import { Button, Card, EmptyState, ErrorBanner, Field, IconButton, SectionTitle, Segmented, Select, Skeleton } from "@/components/ui";
import { Icon } from "@/components/icons";
import { EXTERNAL_LINK_REL, SHORTCUT_LABEL_MAX_LENGTH, SHORTCUT_URL_MAX_LENGTH, SHORTCUTS_MAX } from "@/config";
import { ArchivedMark } from "@/features/archive/ArchiveBanner";
import { PageLink } from "@/features/pages/PageLink";
import { t } from "@/i18n";

type Kind = "page" | "link";
type Direction = "up" | "down";
/** Where the keyboard goes once the list has changed: the moved row's button, or the removed row's neighbour. */
type FocusNext = { moved: string; direction: Direction; removedAt?: never } | { removedAt: number; moved?: never; direction?: never };

const INDENT = "   ";

/** A space's shortcuts as its settings list them: everybody who reads the space sees them, its administrators add, order and remove them. */
export function ShortcutsPanel({ space }: { space: Space }) {
  const s = t.shortcuts;
  const { data: shortcuts, isLoading, error, refetch } = useShortcuts(space.key);
  const move = useMoveShortcut(space.key);
  const remove = useRemoveShortcut(space.key);
  const [notice, setNotice] = useState("");
  const list = useRef<HTMLOListElement>(null);
  const [focusNext, setFocusNext] = useState<FocusNext>();
  const admin = space.can.administer;

  // Applied once the list shows the new order, never before: a button found
  // earlier could be one about to go.
  useEffect(() => {
    if (!focusNext || !shortcuts) return;
    setFocusNext(undefined);
    const rows = Array.from(list.current?.querySelectorAll<HTMLElement>("[data-shortcut-row]") ?? []);
    if (focusNext.moved) {
      // The button pressed stays under the keyboard as its row moves, or its
      // twin when the row reached an end and the pressed one went disabled.
      const row = rows.find((each) => each.dataset.shortcutRow === focusNext.moved);
      const pressed = row?.querySelector<HTMLButtonElement>(`[data-action="move-${focusNext.direction}"]`);
      const other = row?.querySelector<HTMLButtonElement>(`[data-action="move-${focusNext.direction === "up" ? "down" : "up"}"]`);
      (pressed && !pressed.disabled ? pressed : other)?.focus();
      return;
    }
    // A removed row's neighbour takes the keyboard, or the form when none is left.
    const next = rows[Math.min(focusNext.removedAt ?? 0, rows.length - 1)]?.querySelector<HTMLButtonElement>('[data-action="remove-shortcut"]');
    (next ?? document.querySelector<HTMLElement>("[data-shortcut-form] button[aria-pressed='true']"))?.focus();
  }, [focusNext, shortcuts]);

  function shift(all: Shortcut[], index: number, direction: Direction) {
    const shortcut = all[index];
    if (!shortcut) return;
    const rest = all.filter((each) => each.id !== shortcut.id);
    const at = direction === "up" ? index - 1 : index + 1;
    const after = at > 0 ? (rest[at - 1]?.id ?? null) : null;
    setNotice("");
    move.mutate(
      { id: shortcut.id, after },
      {
        onSuccess: (ordered) => {
          const position = ordered.findIndex((each) => each.id === shortcut.id) + 1;
          setNotice(s.moved(shortcutName(shortcut), position, ordered.length));
          setFocusNext({ moved: shortcut.id, direction });
        },
      },
    );
  }

  function drop(all: Shortcut[], index: number) {
    const shortcut = all[index];
    if (!shortcut) return;
    setNotice("");
    remove.mutate(shortcut.id, {
      onSuccess: () => {
        setNotice(s.removed(shortcutName(shortcut)));
        setFocusNext({ removedAt: index });
      },
    });
  }

  return (
    <div className="space-y-4" data-space-shortcuts={space.key}>
      <p className="max-w-xl text-sm text-ink-muted">{s.intro}</p>
      {!admin && <p className="text-sm text-ink-muted">{s.notAdmin}</p>}
      <p role="status" className="text-sm text-ink-muted [&:empty]:hidden" data-shortcuts-notice>
        {notice}
      </p>
      {(move.error ?? remove.error) && <ErrorBanner>{(move.error ?? remove.error)?.message}</ErrorBanner>}
      {error && <ErrorBanner onRetry={() => void refetch()}>{error.message}</ErrorBanner>}
      {isLoading && <Skeleton />}
      {shortcuts && shortcuts.length === 0 && <EmptyState icon={<Icon.Link />} title={admin ? s.emptyAdmin : s.empty} />}
      {shortcuts && shortcuts.length > 0 && (
        <ol ref={list} aria-label={s.list} className="divide-y divide-border rounded-card border border-border bg-surface">
          {shortcuts.map((shortcut, index) => (
            <li key={shortcut.id} className="flex items-center gap-3 px-3 py-2" data-shortcut-row={shortcut.id} data-shortcut-name={shortcutName(shortcut)}>
              <Target shortcut={shortcut} />
              {admin && (
                <div className="flex shrink-0 items-center gap-1">
                  <IconButton
                    icon={<Icon.ChevronUp />}
                    label={s.moveUp(shortcutName(shortcut))}
                    size="sm"
                    disabled={index === 0 || move.isPending}
                    onClick={() => shift(shortcuts, index, "up")}
                    data-action="move-up"
                  />
                  <IconButton
                    icon={<Icon.ChevronDown />}
                    label={s.moveDown(shortcutName(shortcut))}
                    size="sm"
                    disabled={index === shortcuts.length - 1 || move.isPending}
                    onClick={() => shift(shortcuts, index, "down")}
                    data-action="move-down"
                  />
                  <IconButton
                    icon={<Icon.Trash />}
                    label={s.remove(shortcutName(shortcut))}
                    size="sm"
                    disabled={remove.isPending}
                    onClick={() => drop(shortcuts, index)}
                    data-action="remove-shortcut"
                  />
                </div>
              )}
            </li>
          ))}
        </ol>
      )}
      {admin && shortcuts && <AddShortcut space={space} full={shortcuts.length >= SHORTCUTS_MAX} onAdded={(name) => setNotice(s.added(name))} />}
    </div>
  );
}

function Target({ shortcut }: { shortcut: Shortcut }) {
  const s = t.shortcuts;
  const name = shortcutName(shortcut);
  const link = "font-medium text-ink hover:text-accent hover:underline";
  if (shortcut.page) {
    return (
      <div className="min-w-0 flex-1">
        <PageLink spaceKey={shortcut.page.spaceKey} id={shortcut.page.id} title={shortcut.page.title} home={shortcut.page.home} className={link}>
          {name}
        </PageLink>
        {shortcut.page.archived && <ArchivedMark className="ml-2 align-middle" />}
        <span className="block truncate text-xs text-ink-muted">
          {s.pageIn(shortcut.page.spaceKey)}
          {shortcut.label && shortcut.label !== shortcut.page.title ? `: ${shortcut.page.title}` : ""}
        </span>
      </div>
    );
  }
  const href = safeHref(shortcut.url);
  return (
    <div className="min-w-0 flex-1">
      {href ? (
        <a href={href} target="_blank" rel={EXTERNAL_LINK_REL} className={link}>
          {name}
          <span className="sr-only"> {s.opensNewTab}</span>
        </a>
      ) : (
        <span className="font-medium text-ink">{name}</span>
      )}
      <span className="block truncate text-xs text-ink-muted">
        {s.link}: {shortcut.url}
      </span>
    </div>
  );
}

function AddShortcut({ space, full, onAdded }: { space: Space; full: boolean; onAdded: (name: string) => void }) {
  const s = t.shortcuts;
  const add = useAddShortcut(space.key);
  const [kind, setKind] = useState<Kind>("page");
  const [spaceKey, setSpaceKey] = useState(space.key);
  const { data: spaces } = useSpaces();
  const { data: outline, isLoading } = useOutline(spaceKey);
  const [chosen, setChosen] = useState("");
  const pageId = outline?.some((entry) => entry.id === chosen) ? chosen : (outline?.[0]?.id ?? "");
  const [url, setUrl] = useState("");
  const [label, setLabel] = useState("");
  const [urlError, setUrlError] = useState("");
  const fields = add.error instanceof ApiError ? add.error.fields : {};

  function submit(event: FormEvent) {
    event.preventDefault();
    setUrlError("");
    if (kind === "link" && !safeHref(url.trim())) {
      setUrlError(s.badUrl);
      return;
    }
    const input = kind === "page" ? { pageId, label: label.trim() || undefined } : { url: url.trim(), label: label.trim() || undefined };
    add.mutate(input, {
      onSuccess: (made) => {
        setUrl("");
        setLabel("");
        onAdded(shortcutName(made));
      },
    });
  }

  return (
    <Card className="space-y-3 p-4" data-shortcut-form>
      <SectionTitle>{s.addTitle}</SectionTitle>
      {full ? (
        <p className="text-sm text-ink-muted">{s.full(SHORTCUTS_MAX)}</p>
      ) : (
        <form onSubmit={submit} className="space-y-3" noValidate>
          {add.error && !Object.keys(fields).length && <ErrorBanner>{add.error.message}</ErrorBanner>}
          <Segmented<Kind>
            label={s.kind}
            value={kind}
            onChange={(next) => {
              setKind(next);
              setUrlError("");
              add.reset();
            }}
            options={[
              { value: "page", label: s.kindPage, attrs: { "data-shortcut-kind": "page" } },
              { value: "link", label: s.kindLink, attrs: { "data-shortcut-kind": "link" } },
            ]}
          />
          {kind === "page" ? (
            <>
              <Select label={s.space} value={spaceKey} onChange={(event) => setSpaceKey(event.target.value)}>
                {(spaces ?? [space]).map((each) => (
                  <option key={each.key} value={each.key}>
                    {each.name} ({each.key})
                  </option>
                ))}
              </Select>
              <Select
                label={s.page}
                value={pageId}
                onChange={(event) => setChosen(event.target.value)}
                disabled={isLoading}
                hint={isLoading ? s.loadingPages : undefined}
                error={fields.pageId}
              >
                {(outline ?? []).map((entry) => (
                  <option key={entry.id} value={entry.id}>
                    {INDENT.repeat(entry.depth) + entry.title}
                  </option>
                ))}
              </Select>
            </>
          ) : (
            <Field
              label={s.url}
              type="url"
              inputMode="url"
              value={url}
              maxLength={SHORTCUT_URL_MAX_LENGTH}
              onChange={(event) => setUrl(event.target.value)}
              hint={s.urlHint}
              error={urlError || fields.url}
              placeholder="https://"
            />
          )}
          <Field
            label={s.label}
            value={label}
            maxLength={SHORTCUT_LABEL_MAX_LENGTH}
            onChange={(event) => setLabel(event.target.value)}
            hint={kind === "page" ? s.labelPageHint : s.labelLinkHint}
            error={fields.label}
          />
          <Button type="submit" loading={add.isPending} disabled={kind === "page" && !pageId} icon={<Icon.Plus />} data-action="add-shortcut">
            {s.add}
          </Button>
        </form>
      )}
    </Card>
  );
}
