import { useState, type FormEvent } from "react";
import type { Page } from "@/api/pages";
import { useSpaces } from "@/api/spaces";
import { useCopyPage, useMovePage, useOutline, type OutlineEntry } from "@/api/tree";
import { Button, Checkbox, Dialog, ErrorBanner, Field, Select } from "@/components/ui";
import { PAGE_TITLE_MAX_LENGTH } from "@/config";
import { t } from "@/i18n";

// Depth is drawn with spaces a select keeps, since an option holds only text.
const INDENT = "   ";

export interface PlacedPage {
  id: string;
  title: string;
  spaceKey: string;
}

/** The pages a page may go under: all of the outline but, for a move, itself and what is below it. */
export function placesFor(outline: OutlineEntry[], page: PlacedPage, sameSpace: boolean, moving: boolean): OutlineEntry[] {
  if (!moving || !sameSpace) return outline;
  const start = outline.findIndex((entry) => entry.id === page.id);
  if (start < 0) return outline;
  let end = start + 1;
  while (end < outline.length && outline[end]!.depth > outline[start]!.depth) end++;
  return [...outline.slice(0, start), ...outline.slice(end)];
}

/**
 * Moves or copies a page by choosing a space and a parent from lists, which
 * is how the keyboard does what dragging in the tree does.
 */
export function PlaceDialog({ page, mode, onClose, onDone }: { page: PlacedPage; mode: "move" | "copy"; onClose: () => void; onDone: (page: Page) => void }) {
  const moving = mode === "move";
  const { data: spaces } = useSpaces();
  const [spaceKey, setSpaceKey] = useState(page.spaceKey);
  const { data: outline, isLoading } = useOutline(spaceKey);
  const places = outline ? placesFor(outline, page, spaceKey === page.spaceKey, moving) : [];
  const [chosen, setChosen] = useState<string>("");
  const parentId = places.some((entry) => entry.id === chosen) ? chosen : (places[0]?.id ?? "");
  const [withChildren, setWithChildren] = useState(true);
  const [title, setTitle] = useState(page.title);
  const move = useMovePage();
  const copy = useCopyPage();
  const pending = move.isPending || copy.isPending;
  const error = move.error ?? copy.error;

  function submit(event: FormEvent) {
    event.preventDefault();
    if (!parentId) return;
    const done = { onSuccess: (placed: Page) => onDone(placed) };
    if (moving) move.mutate({ id: page.id, parentId, withChildren }, done);
    else copy.mutate({ id: page.id, parentId, withChildren, title }, done);
  }

  return (
    <Dialog title={moving ? t.page.moveTitle(page.title) : t.page.copyTitle(page.title)} onClose={onClose} data-place-dialog={mode}>
      <form onSubmit={submit} className="space-y-3">
        {error && <ErrorBanner>{error.message}</ErrorBanner>}
        <Select label={t.page.space} value={spaceKey} onChange={(event) => setSpaceKey(event.target.value)}>
          {(spaces ?? []).map((space) => (
            <option key={space.key} value={space.key}>
              {space.name} ({space.key})
            </option>
          ))}
        </Select>
        <Select
          label={t.page.parent}
          value={parentId}
          onChange={(event) => setChosen(event.target.value)}
          disabled={isLoading}
          hint={isLoading ? t.page.loadingPlaces : undefined}
        >
          {places.map((entry) => (
            <option key={entry.id} value={entry.id}>
              {INDENT.repeat(entry.depth) + entry.title}
            </option>
          ))}
        </Select>
        {!moving && <Field label={t.page.copyName} value={title} maxLength={PAGE_TITLE_MAX_LENGTH} onChange={(event) => setTitle(event.target.value)} />}
        <Checkbox
          label={moving ? t.page.moveChildren : t.page.copyChildren}
          checked={withChildren}
          onChange={(event) => setWithChildren(event.target.checked)}
        />
        <div className="flex justify-end gap-2 pt-1">
          <Button type="button" variant="secondary" onClick={onClose}>
            {t.page.cancel}
          </Button>
          <Button type="submit" loading={pending} disabled={!parentId} data-action={moving ? "confirm-move" : "confirm-copy"}>
            {moving ? t.page.moveSubmit : t.page.copySubmit}
          </Button>
        </div>
      </form>
    </Dialog>
  );
}
