import { useId, useState, type FormEvent } from "react";
import { useExcerpts } from "@/api/excerpts";
import { useSpaces } from "@/api/spaces";
import { useOutline } from "@/api/tree";
import { Button, Dialog, Select } from "@/components/ui";
import { t } from "@/i18n";

// Depth is drawn with spaces a select keeps, since an option holds only text.
const INDENT = "   ";
const WHOLE = "";

/** What an include shows: a whole page, or one named excerpt of it. */
export interface IncludeChoice {
  pageId: string;
  pageTitle: string;
  spaceKey: string;
  /** Null for the whole page. */
  excerptId: string | null;
  excerptName: string | null;
}

/**
 * Chooses a page by its space and its place in the tree, then the whole page
 * or one of its excerpts, listed by name with the start of their words.
 * A page cannot include itself, so the page being edited is left out.
 */
export function ExcerptPicker({
  initialSpaceKey,
  excludePageId,
  onPick,
  onClose,
}: {
  initialSpaceKey?: string;
  excludePageId?: string;
  onPick: (choice: IncludeChoice) => void;
  onClose: () => void;
}) {
  const p = t.editor.excerpt.picker;
  const id = useId();
  const { data: spaces } = useSpaces();
  const [spaceKey, setSpaceKey] = useState(initialSpaceKey ?? "");
  const space = spaceKey || spaces?.[0]?.key || "";
  const { data: outline, isLoading } = useOutline(space || undefined);
  const pages = (outline ?? []).filter((entry) => entry.id !== excludePageId);
  const [chosenPage, setChosenPage] = useState("");
  const pageId = pages.some((entry) => entry.id === chosenPage) ? chosenPage : (pages[0]?.id ?? "");
  const excerpts = useExcerpts(pageId || undefined);
  const [chosenExcerpt, setChosenExcerpt] = useState(WHOLE);
  const excerpt = excerpts.data?.find((each) => each.id === chosenExcerpt) ?? null;

  function submit(event: FormEvent) {
    // The page's own form is this one's ancestor in React's tree.
    event.preventDefault();
    event.stopPropagation();
    const page = pages.find((entry) => entry.id === pageId);
    if (!page) return;
    onPick({ pageId: page.id, pageTitle: page.title, spaceKey: space, excerptId: excerpt?.id ?? null, excerptName: excerpt?.name ?? null });
  }

  return (
    <Dialog title={p.dialog} onClose={onClose} data-excerpt-picker="">
      <form onSubmit={submit} className="space-y-3" noValidate>
        <Select
          label={p.space}
          value={space}
          onChange={(event) => {
            setSpaceKey(event.target.value);
            setChosenPage("");
            setChosenExcerpt(WHOLE);
          }}
        >
          {(spaces ?? []).map((each) => (
            <option key={each.key} value={each.key}>
              {each.name} ({each.key})
            </option>
          ))}
        </Select>
        <Select
          label={p.page}
          value={pageId}
          disabled={isLoading || pages.length === 0}
          hint={!isLoading && pages.length === 0 ? p.noPages : undefined}
          onChange={(event) => {
            setChosenPage(event.target.value);
            setChosenExcerpt(WHOLE);
          }}
        >
          {pages.map((entry) => (
            <option key={entry.id} value={entry.id}>
              {INDENT.repeat(entry.depth) + entry.title}
            </option>
          ))}
        </Select>
        <fieldset className="space-y-1" disabled={!pageId}>
          <legend className="text-sm font-medium text-ink-muted">{p.what}</legend>
          <label className="flex items-start gap-2 text-sm text-ink">
            <input
              type="radio"
              name={`${id}-what`}
              checked={!excerpt}
              onChange={() => setChosenExcerpt(WHOLE)}
              className="mt-1 accent-accent"
              data-excerpt-choice=""
            />
            {p.wholePage}
          </label>
          {excerpts.isLoading && pageId && <p className="text-sm text-ink-muted">{p.loading}</p>}
          {excerpts.data?.length === 0 && <p className="text-sm text-ink-muted">{p.none}</p>}
          {excerpts.data?.map((each) => (
            <label key={each.id} className="flex items-start gap-2 text-sm text-ink">
              <input
                type="radio"
                name={`${id}-what`}
                checked={excerpt?.id === each.id}
                onChange={() => setChosenExcerpt(each.id)}
                className="mt-1 accent-accent"
                data-excerpt-choice={each.id}
              />
              <span>
                <span className="font-medium">{each.name}</span>
                {each.text && <span className="block text-ink-muted">{each.text}</span>}
              </span>
            </label>
          ))}
        </fieldset>
        <div className="flex justify-end gap-2 pt-1">
          <Button type="submit" disabled={!pageId} data-action="pick-include">
            {p.choose}
          </Button>
        </div>
      </form>
    </Dialog>
  );
}
