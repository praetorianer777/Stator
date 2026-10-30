import { useId, useState, type FormEvent, type KeyboardEvent } from "react";
import { exciterLacks, useExciterAccount, useExciterSearch, type ExciterHit } from "@/api/exciter";
import { Button, cx, Dialog, Input, Labelled } from "@/components/ui";
import { describedBy } from "@/components/ui/controls";
import { EXCITER_FEATURE_CAPABILITIES, EXCITER_SEARCH_DEBOUNCE_MS, EXCITER_SEARCH_MAX_LENGTH, EXCITER_SEARCH_MIN_LENGTH } from "@/config";
import { useFollowActive } from "@/features/editor/useFollowActive";
import { t } from "@/i18n";
import { useDebounced } from "@/lib/debounce";
import { ConnectExciterLink } from "./connectLink";
import { RequirementRevision, RequirementStatusLozenge } from "./RequirementChip";

/** What the picker has for what was typed. */
export type PickerState = "outdated" | "connect" | "hint" | "short" | "looking" | "unreachable" | "noMatch" | "found";

const NONE: ExciterHit[] = [];

/** Finds requirements by the start of their key or title, debounced, as the author may see them in Exciter. */
export function useRequirementFinder(text: string): { state: PickerState; hits: ExciterHit[] } {
  const account = useExciterAccount();
  const connected = Boolean(account.data?.configured && account.data.connected && account.data.status !== "rejected");
  const outdated = exciterLacks(account.data, "requirements");
  const typed = text.trim().slice(0, EXCITER_SEARCH_MAX_LENGTH);
  const words = typed.length >= EXCITER_SEARCH_MIN_LENGTH ? typed : "";
  const settled = useDebounced(words, EXCITER_SEARCH_DEBOUNCE_MS);
  const found = useExciterSearch(settled, connected && !outdated && words !== "" && settled === words);

  let state: PickerState;
  if (outdated) state = "outdated";
  else if (account.isPending) state = "looking";
  else if (!connected) state = "connect";
  else if (typed === "") state = "hint";
  else if (words === "") state = "short";
  else if (settled !== words || found.isPending) state = "looking";
  else if (found.isError || found.data?.status === "unreachable") state = "unreachable";
  else if (found.data?.status === "outdated") state = "outdated";
  else if (found.data?.status !== "ok") state = "connect";
  else if (found.data.requirements.length === 0) state = "noMatch";
  else state = "found";
  return { state, hits: state === "found" ? (found.data?.requirements ?? NONE) : NONE };
}

function PickerStatus({ state, count }: { state: PickerState; count: number }) {
  const p = t.exciter.picker;
  switch (state) {
    case "outdated":
      return <>{t.exciter.capabilities.outdated(t.exciter.capabilities.features.requirements, EXCITER_FEATURE_CAPABILITIES.requirements)}</>;
    case "connect":
      return (
        <span>
          {p.connect} <ConnectExciterLink className="underline">{p.connectLink}</ConnectExciterLink>
        </span>
      );
    case "hint":
      return <>{p.hint}</>;
    case "short":
      return <>{p.short(EXCITER_SEARCH_MIN_LENGTH)}</>;
    case "looking":
      return <>{p.looking}</>;
    case "unreachable":
      return <>{p.unreachable}</>;
    case "noMatch":
      return <>{p.noMatch}</>;
    case "found":
      return <>{p.found(count)}</>;
  }
}

/**
 * Finds a requirement by words of its title or its key and inserts the one
 * picked, so only a requirement Exciter found for the author reaches the
 * page. One picker serves the chip and the block, which title it apart.
 */
export function RequirementPicker({ title, onInsert, onClose }: { title: string; onInsert: (key: string) => void; onClose: () => void }) {
  const p = t.exciter.picker;
  const id = useId();
  const [text, setText] = useState("");
  const [active, setActive] = useState(0);
  const { state, hits } = useRequirementFinder(text);
  const follow = useFollowActive(active);
  const current = hits.length > 0 ? Math.min(active, hits.length - 1) : -1;

  function submit(event: FormEvent) {
    event.preventDefault();
    // The page's own form is this one's ancestor in React's tree.
    event.stopPropagation();
    const picked = hits[current];
    if (picked) onInsert(picked.key);
  }

  function move(event: KeyboardEvent<HTMLInputElement>) {
    if (hits.length === 0 || (event.key !== "ArrowDown" && event.key !== "ArrowUp")) return;
    event.preventDefault();
    const delta = event.key === "ArrowDown" ? 1 : -1;
    setActive((current + delta + hits.length) % hits.length);
  }

  const fieldId = `${id}-words`;
  const statusId = `${id}-status`;
  const listId = `${id}-requirements`;
  return (
    <Dialog
      open
      title={title}
      onClose={onClose}
      attrs={{ "data-exciter-picker": "" }}
      footer={
        <>
          <Button type="button" variant="secondary" onClick={onClose}>
            {p.cancel}
          </Button>
          <Button type="submit" form="requirement-picker-form" disabled={current < 0} data-action="insert-exciter-requirement">
            {p.insert}
          </Button>
        </>
      }
    >
      <form id="requirement-picker-form" onSubmit={submit} className="space-y-3" noValidate>
        <Labelled id={fieldId} label={p.field} hint={p.fieldHint}>
          <Input
            id={fieldId}
            role="combobox"
            aria-expanded={hits.length > 0}
            aria-controls={listId}
            aria-autocomplete="list"
            aria-activedescendant={current >= 0 ? `${listId}-${current}` : undefined}
            value={text}
            autoComplete="off"
            spellCheck={false}
            maxLength={EXCITER_SEARCH_MAX_LENGTH}
            aria-describedby={[describedBy(fieldId, p.fieldHint), statusId].filter(Boolean).join(" ")}
            onKeyDown={move}
            onChange={(event) => {
              setText(event.target.value);
              setActive(0);
            }}
          />
        </Labelled>
        <div id={statusId} role="status" className="min-h-6 text-sm text-ink-muted" data-picker-state={state}>
          <PickerStatus state={state} count={hits.length} />
        </div>
        <div
          ref={follow}
          id={listId}
          role="listbox"
          aria-label={p.results}
          className={hits.length > 0 ? "max-h-64 overflow-y-hidden" : "hidden"}
          data-requirement-options=""
        >
          {hits.map((hit, i) => (
            <div
              key={hit.key}
              id={`${listId}-${i}`}
              role="option"
              aria-selected={i === current}
              tabIndex={-1}
              data-requirement-option={hit.key}
              // The field keeps focus, so a click picks without taking it.
              onMouseDown={(e) => {
                e.preventDefault();
                onInsert(hit.key);
              }}
              onMouseEnter={() => setActive(i)}
              className={cx(
                "flex cursor-pointer items-center gap-2 rounded-control px-2 py-1 text-sm",
                i === current ? "bg-surface-raised text-ink" : "text-ink-muted",
              )}
            >
              <span className={cx("shrink-0 font-medium text-ink", hit.trashed && "line-through")}>{hit.key}</span>
              <span className="min-w-0 flex-1 truncate">{hit.title}</span>
              <RequirementStatusLozenge status={hit.status} />
              <RequirementRevision revision={hit.revision} />
              <span className="shrink-0 text-xs text-ink-muted">
                <span className="sr-only">{p.project} </span>
                {hit.projectName}
              </span>
            </div>
          ))}
        </div>
      </form>
    </Dialog>
  );
}
