import { useId, useRef, useState, type FormEvent, type KeyboardEvent } from "react";
import { ApiError } from "@/api/client";
import { exciterLacks, useExciterAccount, useExciterProjects, useExciterQueryCheck, useExciterSuggest } from "@/api/exciter";
import { Button, cx, Dialog, Input, Labelled, SelectInput, Textarea } from "@/components/ui";
import { describedBy } from "@/components/ui/controls";
import { EXCITER_LIST_MAX_LIMIT, EXCITER_QUERY_CHECK_DEBOUNCE_MS, EXCITER_QUERY_MAX_LENGTH, EXCITER_SUGGEST_DEBOUNCE_MS } from "@/config";
import { MarkedQuery } from "@/features/armature/IssueList";
import { t } from "@/i18n";
import { useDebounced } from "@/lib/debounce";
import type { RequirementListSettings } from "./RequirementList";
import { normalizeProjectKey } from "./requirementKeys";

type Problems = Partial<Record<"project" | "query" | "limit", string>>;

/** The settings as the server takes them, or a sentence for each field that is wrong. */
export function checkListSettings(project: string, query: string, limit: string): { settings: RequirementListSettings } | { problems: Problems } {
  const d = t.exciter.listDialog;
  const problems: Problems = {};
  const key = normalizeProjectKey(project);
  if (!key) problems.project = d.projectNone;
  if ([...query].length > EXCITER_QUERY_MAX_LENGTH) problems.query = d.queryLong(EXCITER_QUERY_MAX_LENGTH);
  const rows = Number(limit);
  if (!Number.isInteger(rows) || rows < 1 || rows > EXCITER_LIST_MAX_LIMIT) problems.limit = d.limitRange(EXCITER_LIST_MAX_LIMIT);
  if (Object.keys(problems).length > 0 || !key) return { problems };
  return { settings: { project: key, query, limit: rows } };
}

/** Writes word over the characters from to to of text, counted as characters, and where the caret goes after it. */
export function completeWith(text: string, from: number, to: number, word: string): { text: string; caret: number } {
  const chars = [...text];
  const before = chars.slice(0, from).join("");
  const after = chars.slice(to).join("");
  const spaced = after.startsWith(" ") ? word : `${word} `;
  return { text: before + spaced + after, caret: [...before].length + [...spaced].length };
}

/** The caret of a field, in characters rather than UTF-16 units, as Exciter counts it. */
function caretOf(field: HTMLTextAreaElement): number {
  return [...field.value.slice(0, field.selectionStart ?? field.value.length)].length;
}

/** The character position as the UTF-16 offset a field's selection takes. */
function offsetOf(text: string, chars: number): number {
  return [...text].slice(0, chars).join("").length;
}

/**
 * Asks for a requirement list's project, query and most rows. The query is
 * checked with Exciter as it is typed, and completed at the caret from what
 * Exciter suggests there.
 */
export function RequirementListDialog({
  initial,
  isNew,
  onSave,
  onClose,
}: {
  initial: RequirementListSettings;
  isNew: boolean;
  onSave: (settings: RequirementListSettings) => void;
  onClose: () => void;
}) {
  const d = t.exciter.listDialog;
  const id = useId();
  const account = useExciterAccount();
  const connected = Boolean(account.data?.connected && account.data.status !== "rejected");
  const listsOffered = !exciterLacks(account.data, "lists");
  const projectsOffered = !exciterLacks(account.data, "projects");
  const projects = useExciterProjects(connected && projectsOffered);
  const choices = projects.data?.status === "ok" ? projects.data.projects : null;
  const [project, setProject] = useState(initial.project);
  const [query, setQuery] = useState(initial.query);
  const [limit, setLimit] = useState(String(initial.limit));
  const [problems, setProblems] = useState<Problems>({});
  const field = useRef<HTMLTextAreaElement>(null);
  const [caret, setCaret] = useState<number | null>(null);
  const [active, setActive] = useState(0);
  const [dismissed, setDismissed] = useState(false);

  const projectKey = normalizeProjectKey(project) ?? "";
  const asks = connected && listsOffered && projectKey !== "";
  const settled = useDebounced(query, EXCITER_QUERY_CHECK_DEBOUNCE_MS);
  const typing = settled !== query;
  const check = useExciterQueryCheck(projectKey, settled, asks && [...settled].length <= EXCITER_QUERY_MAX_LENGTH);
  const bad = check.error instanceof ApiError && check.error.code === "bad_query" ? check.error : null;

  // A string, so the debounce settles: an object would be new every render.
  const typed = caret === null ? "" : `${caret} ${query}`;
  const settledAt = useDebounced(typed, EXCITER_SUGGEST_DEBOUNCE_MS);
  const at = settledAt === "" ? null : { at: Number(settledAt.slice(0, settledAt.indexOf(" "))), q: settledAt.slice(settledAt.indexOf(" ") + 1) };
  const current = at !== null && settledAt === typed;
  const suggest = useExciterSuggest(projectKey, at?.q ?? "", at?.at ?? 0, asks && at !== null && !dismissed);
  const words = current && !dismissed ? (suggest.data?.words ?? []) : [];
  const completion = suggest.data?.completion;
  const shown = Math.min(active, Math.max(words.length - 1, 0));

  function pick(text: string) {
    if (!completion) return;
    const next = completeWith(query, completion.from, completion.to, text);
    setQuery(next.text);
    setCaret(next.caret);
    setActive(0);
    setProblems(({ query: _, ...rest }) => rest);
    // The field keeps focus; the caret goes after what was written.
    requestAnimationFrame(() => {
      const el = field.current;
      if (!el) return;
      const offset = offsetOf(next.text, next.caret);
      el.setSelectionRange(offset, offset);
    });
  }

  function keys(event: KeyboardEvent<HTMLTextAreaElement>) {
    if (words.length === 0) return;
    switch (event.key) {
      case "ArrowDown":
      case "ArrowUp": {
        event.preventDefault();
        const delta = event.key === "ArrowDown" ? 1 : -1;
        setActive((shown + delta + words.length) % words.length);
        break;
      }
      case "Enter":
      case "Tab": {
        const word = words[shown];
        if (!word) return;
        event.preventDefault();
        pick(word.text);
        break;
      }
      case "Escape":
        // The completion closes; the dialog stays.
        event.preventDefault();
        event.stopPropagation();
        setDismissed(true);
        break;
    }
  }

  function submit(event: FormEvent) {
    event.preventDefault();
    // The page's own form is this one's ancestor in React's tree.
    event.stopPropagation();
    const checked = checkListSettings(project, query, limit);
    if ("problems" in checked) {
      setProblems(checked.problems);
      return;
    }
    if (bad && !typing) {
      setProblems({ query: d.queryBad });
      return;
    }
    onSave(checked.settings);
  }

  const projectId = `${id}-project`;
  const queryId = `${id}-query`;
  const checkId = `${id}-check`;
  const wordsId = `${id}-words`;
  const limitId = `${id}-limit`;
  let checkText: string | null = null;
  if (!connected) checkText = d.cannotCheck;
  else if (projectKey === "") checkText = null;
  else if (typing || check.isFetching) checkText = d.checking;
  else if (bad) checkText = t.exciter.list.badQuery(bad.message, bad.position);
  else if (check.data?.status === "ok") checkText = check.data.found ? d.matches(check.data.total) : t.exciter.list.unseen(projectKey);
  else if (check.data || check.isError) checkText = d.cannotCheck;

  const follow = (event: { currentTarget: HTMLTextAreaElement }) => {
    setCaret(caretOf(event.currentTarget));
    setDismissed(false);
  };

  return (
    <Dialog title={isNew ? d.titleNew : d.titleEdit} onClose={onClose} wide data-requirement-list-dialog="">
      <form onSubmit={submit} className="space-y-4" noValidate>
        <Labelled id={projectId} label={d.project} hint={choices ? undefined : d.projectHint} error={problems.project} className="max-w-sm">
          {choices ? (
            <SelectInput
              id={projectId}
              value={projectKey}
              invalid={Boolean(problems.project)}
              aria-describedby={describedBy(projectId, undefined, problems.project)}
              onChange={(event) => {
                setProject(event.target.value);
                setProblems(({ project: _, ...rest }) => rest);
              }}
              className="w-full"
            >
              <option value="">{d.projectChoose}</option>
              {choices.map((p) => (
                <option key={p.key} value={p.key}>
                  {`${p.name} (${p.key})`}
                </option>
              ))}
              {projectKey !== "" && !choices.some((p) => p.key === projectKey) && <option value={projectKey}>{projectKey}</option>}
            </SelectInput>
          ) : (
            <Input
              id={projectId}
              value={project}
              autoComplete="off"
              spellCheck={false}
              invalid={Boolean(problems.project)}
              aria-describedby={describedBy(projectId, d.projectHint, problems.project)}
              onChange={(event) => {
                setProject(event.target.value);
                setProblems(({ project: _, ...rest }) => rest);
              }}
            />
          )}
        </Labelled>
        <Labelled id={queryId} label={d.query} hint={d.queryHint} error={problems.query}>
          <div className="relative">
            <Textarea
              ref={field}
              id={queryId}
              rows={3}
              value={query}
              spellCheck={false}
              invalid={Boolean(problems.query)}
              aria-invalid={problems.query ? true : undefined}
              aria-autocomplete="list"
              aria-controls={wordsId}
              aria-activedescendant={words.length > 0 ? `${wordsId}-${shown}` : undefined}
              aria-describedby={[describedBy(queryId, d.queryHint, problems.query), checkId].filter(Boolean).join(" ")}
              onKeyDown={keys}
              onKeyUp={(event) => {
                if (event.key === "ArrowLeft" || event.key === "ArrowRight" || event.key === "Home" || event.key === "End") follow(event);
              }}
              onClick={follow}
              onFocus={follow}
              onBlur={() => setCaret(null)}
              onChange={(event) => {
                setQuery(event.target.value);
                setActive(0);
                follow(event);
                setProblems(({ query: _, ...rest }) => rest);
              }}
            />
            <div
              id={wordsId}
              role="listbox"
              aria-label={d.suggestions}
              className={cx(
                "absolute inset-x-0 top-full z-10 mt-1 max-h-48 overflow-y-auto rounded-control border border-border bg-surface p-1 shadow-lg",
                words.length === 0 && "hidden",
              )}
              data-query-suggestions=""
            >
              {words.map((word, i) => (
                <div
                  key={word.text}
                  id={`${wordsId}-${i}`}
                  role="option"
                  aria-selected={i === shown}
                  tabIndex={-1}
                  data-query-word={word.text}
                  // The field keeps focus, so a click completes without taking it.
                  onMouseDown={(event) => {
                    event.preventDefault();
                    pick(word.text);
                  }}
                  onMouseEnter={() => setActive(i)}
                  className={cx(
                    "flex cursor-pointer items-baseline gap-2 rounded-control px-2 py-1 text-sm",
                    i === shown ? "bg-surface-raised text-ink" : "text-ink-muted",
                  )}
                >
                  <code className="text-ink">{word.label || word.text}</code>
                  {word.detail && <span className="min-w-0 truncate text-xs">{word.detail}</span>}
                </div>
              ))}
            </div>
          </div>
        </Labelled>
        <div id={checkId} role="status" className="min-h-6 space-y-1 text-sm text-ink-muted" data-query-check={bad ? "bad_query" : (check.data?.status ?? "")}>
          {checkText && <p className={bad ? "text-danger" : undefined}>{checkText}</p>}
          {bad && !typing && <MarkedQuery query={settled} position={bad.position} />}
        </div>
        <Labelled id={limitId} label={d.limit} hint={d.limitHint(EXCITER_LIST_MAX_LIMIT)} error={problems.limit} className="max-w-48">
          <Input
            id={limitId}
            type="number"
            inputMode="numeric"
            min={1}
            max={EXCITER_LIST_MAX_LIMIT}
            value={limit}
            invalid={Boolean(problems.limit)}
            aria-invalid={problems.limit ? true : undefined}
            aria-describedby={describedBy(limitId, d.limitHint(EXCITER_LIST_MAX_LIMIT), problems.limit)}
            onChange={(event) => {
              setLimit(event.target.value);
              setProblems(({ limit: _, ...rest }) => rest);
            }}
          />
        </Labelled>
        <div className="flex justify-end gap-2 pt-1">
          <Button type="button" variant="secondary" onClick={onClose}>
            {d.cancel}
          </Button>
          <Button type="submit" data-action="save-requirement-list">
            {isNew ? d.insert : d.save}
          </Button>
        </div>
      </form>
    </Dialog>
  );
}
