import { useId, useState, type FormEvent, type KeyboardEvent } from "react";
import { Button, Dialog, Input, Labelled } from "@/components/ui";
import { describedBy } from "@/components/ui/controls";
import { t } from "@/i18n";
import { useFollowActive } from "@/features/editor/useFollowActive";
import { FinderStatus, IssueOptions, useIssueFinder } from "./issueFinder";

export { pickedKey } from "./issueFinder";

/**
 * Finds an issue by words of its summary, its key or its address, and
 * inserts the one picked, so only an issue Armature found for the author
 * reaches the page.
 */
export function IssuePicker({ onInsert, onClose }: { onInsert: (key: string) => void; onClose: () => void }) {
  const p = t.armature.picker;
  const id = useId();
  const [text, setText] = useState("");
  const [active, setActive] = useState(0);
  const { state, issues, remember } = useIssueFinder(text);
  const follow = useFollowActive(active);
  const current = issues.length > 0 ? Math.min(active, issues.length - 1) : -1;

  function insert(key: string) {
    remember(key);
    onInsert(key);
  }

  function submit(event: FormEvent) {
    event.preventDefault();
    // The page's own form is this one's ancestor in React's tree.
    event.stopPropagation();
    const picked = issues[current];
    if (picked) insert(picked.key);
  }

  function move(event: KeyboardEvent<HTMLInputElement>) {
    if (issues.length === 0 || (event.key !== "ArrowDown" && event.key !== "ArrowUp")) return;
    event.preventDefault();
    const delta = event.key === "ArrowDown" ? 1 : -1;
    setActive((current + delta + issues.length) % issues.length);
  }

  const fieldId = `${id}-key`;
  const statusId = `${id}-status`;
  const listId = `${id}-issues`;
  const problem = state.kind === "notFound" ? p.notFound(state.key) : state.kind === "unreachable" ? p.unreachable : undefined;
  return (
    <Dialog
      open
      title={p.title}
      onClose={onClose}
      attrs={{ "data-issue-picker": "" }}
      footer={
        <>
          <Button type="button" variant="secondary" onClick={onClose}>
            {p.cancel}
          </Button>
          <Button type="submit" form="issue-picker-form" disabled={current < 0} data-action="insert-issue">
            {p.insert}
          </Button>
        </>
      }
    >
      <form id="issue-picker-form" onSubmit={submit} className="space-y-3" noValidate>
        <Labelled id={fieldId} label={p.field} hint={p.hint} error={problem}>
          <Input
            id={fieldId}
            role="combobox"
            aria-expanded={issues.length > 0}
            aria-controls={listId}
            aria-autocomplete="list"
            aria-activedescendant={current >= 0 ? `${listId}-${current}` : undefined}
            value={text}
            autoComplete="off"
            spellCheck={false}
            invalid={Boolean(problem)}
            aria-invalid={problem ? true : undefined}
            aria-describedby={[describedBy(fieldId, p.hint, problem), statusId].filter(Boolean).join(" ")}
            onKeyDown={move}
            onChange={(event) => {
              setText(event.target.value);
              setActive(0);
            }}
          />
        </Labelled>
        <div id={statusId} role="status" className="min-h-6 text-sm text-ink-muted" data-picker-state={state.kind}>
          {!problem && <FinderStatus state={state} count={issues.length} />}
        </div>
        <IssueOptions
          id={listId}
          label={t.armature.find.results}
          issues={issues}
          active={current}
          onPick={(issue) => insert(issue.key)}
          onHover={setActive}
          listRef={follow}
          className={issues.length > 0 ? "max-h-64 overflow-y-hidden" : "hidden"}
        />
      </form>
    </Dialog>
  );
}
