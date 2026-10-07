import { useId, useState, type FormEvent } from "react";
import { useSpaces } from "@/api/spaces";
import { Button, Dialog, Select } from "@/components/ui";
import { PAGE_LIST_LIMIT_CHOICES } from "@/config";
import { t } from "@/i18n";
import type { BlogPostsSettings } from "./blogPosts";

type Scope = "this" | "other" | "every";

/**
 * Asks whose posts a latest blog posts block lists and how many. This space
 * is stored by its key, so the block reads the same wherever it is included.
 */
export function BlogPostsDialog({
  initial,
  currentSpace,
  isNew,
  onSave,
  onClose,
}: {
  initial: BlogPostsSettings;
  /** The key of the space the page is in, which "this space" names; none outside a page. */
  currentSpace: string | undefined;
  isNew: boolean;
  onSave: (settings: BlogPostsSettings) => void;
  onClose: () => void;
}) {
  const d = t.blogPosts.dialog;
  const id = useId();
  const spaces = useSpaces();
  const [scope, setScope] = useState<Scope>(
    initial.space === null ? "every" : currentSpace && initial.space === currentSpace ? "this" : currentSpace || initial.space ? "other" : "every",
  );
  const [other, setOther] = useState(initial.space && initial.space !== currentSpace ? initial.space : "");
  const [limit, setLimit] = useState(initial.limit);
  const [problem, setProblem] = useState<string>();

  function submit(event: FormEvent) {
    event.preventDefault();
    // The page's own form is this one's ancestor in React's tree.
    event.stopPropagation();
    if (scope === "other" && !other) {
      setProblem(d.spaceNone);
      return;
    }
    const space = scope === "every" ? null : scope === "this" ? (currentSpace ?? null) : other;
    onSave({ space, limit });
  }

  const scopes: Array<{ value: Scope; label: string }> = [
    ...(currentSpace ? [{ value: "this" as const, label: d.thisSpace(currentSpace) }] : []),
    { value: "other", label: d.otherSpace },
    { value: "every", label: d.everySpace },
  ];
  return (
    <Dialog title={isNew ? d.new : d.edit} onClose={onClose} data-blog-posts-dialog="">
      <form onSubmit={submit} className="space-y-4" noValidate>
        <fieldset className="space-y-1">
          <legend className="text-sm font-medium text-ink-muted">{d.scope}</legend>
          {scopes.map((s) => (
            <label key={s.value} className="flex items-center gap-2 text-sm text-ink">
              <input
                type="radio"
                name={`${id}-scope`}
                checked={scope === s.value}
                onChange={() => {
                  setScope(s.value);
                  setProblem(undefined);
                }}
                className="accent-accent"
                data-blog-scope={s.value}
              />
              {s.label}
            </label>
          ))}
        </fieldset>
        {scope === "other" && (
          <Select
            label={d.space}
            value={other}
            error={problem}
            onChange={(event) => {
              setOther(event.target.value);
              setProblem(undefined);
            }}
          >
            <option value="">{d.spaceNone}</option>
            {other && !spaces.data?.some((s) => s.key === other) && <option value={other}>{other}</option>}
            {(spaces.data ?? [])
              .filter((s) => s.key !== currentSpace)
              .map((s) => (
                <option key={s.key} value={s.key}>
                  {s.name} ({s.key})
                </option>
              ))}
          </Select>
        )}
        <Select label={d.limit} value={String(limit)} onChange={(event) => setLimit(Number(event.target.value))}>
          {[...new Set([...PAGE_LIST_LIMIT_CHOICES, initial.limit])]
            .sort((a, b) => a - b)
            .map((n) => (
              <option key={n} value={n}>
                {d.limitChoice(n)}
              </option>
            ))}
        </Select>
        <div className="flex justify-end gap-2 pt-1">
          <Button type="button" variant="secondary" onClick={onClose}>
            {d.cancel}
          </Button>
          <Button type="submit" data-action="save-blog-posts">
            {isNew ? d.insert : d.save}
          </Button>
        </div>
      </form>
    </Dialog>
  );
}
