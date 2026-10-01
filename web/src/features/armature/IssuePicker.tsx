import { useId, useState, type FormEvent } from "react";
import { useArmatureAccount, useArmatureIssue } from "@/api/armature";
import { Button, Dialog, Input, Labelled } from "@/components/ui";
import { describedBy } from "@/components/ui/controls";
import { ARMATURE_PICKER_DEBOUNCE_MS, ARMATURE_SECTION_ID, PROFILE_PATH } from "@/config";
import { t } from "@/i18n";
import { useDebounced } from "@/lib/debounce";
import { StatusLozenge, TypeIcon } from "./IssueChip";
import { keyFromIssueUrl, normalizeKey } from "./issueKeys";

/** What the picker found for what was typed, as one state the form reads. */
export type PickerState =
  | { kind: "empty" }
  | { kind: "notAKey" }
  | { kind: "connect" }
  | { kind: "looking" }
  | { kind: "unreachable" }
  | { kind: "notFound"; key: string }
  | { kind: "found"; key: string; summary: string; type: { icon: string; name: string }; status: { name: string; category: string } };

/** The key a person typed or pasted: a key in any case, or an issue address of the connected Armature. */
export function pickedKey(text: string, baseUrl: string | null): string | null {
  return normalizeKey(text) ?? keyFromIssueUrl(text, baseUrl);
}

/**
 * Asks for an issue by key or address and inserts it once Armature finds it
 * for the author, so a typo never reaches the page.
 */
export function IssuePicker({ onInsert, onClose }: { onInsert: (key: string) => void; onClose: () => void }) {
  const p = t.armature.picker;
  const id = useId();
  const [text, setText] = useState("");
  const [tried, setTried] = useState(false);
  const account = useArmatureAccount();
  const baseUrl = account.data?.configured ? (account.data.baseUrl ?? null) : null;
  const connected = Boolean(account.data?.connected && account.data.status !== "rejected");
  const typed = pickedKey(text, baseUrl);
  const key = useDebounced(typed, ARMATURE_PICKER_DEBOUNCE_MS);
  const found = useArmatureIssue(key ?? "", connected && key !== null && key === typed);

  let state: PickerState;
  if (!connected) state = { kind: "connect" };
  else if (!text.trim()) state = { kind: "empty" };
  else if (!typed) state = { kind: "notAKey" };
  else if (key !== typed || found.isPending) state = { kind: "looking" };
  else if (found.isError || found.data?.status === "unreachable") state = { kind: "unreachable" };
  else if (found.data?.status !== "ok") state = { kind: "connect" };
  else if (!found.data.issue) state = { kind: "notFound", key: typed };
  else {
    const issue = found.data.issue;
    state = { kind: "found", key: issue.key, summary: issue.summary, type: issue.type, status: issue.status };
  }

  function submit(event: FormEvent) {
    event.preventDefault();
    // The page's own form is this one's ancestor in React's tree.
    event.stopPropagation();
    setTried(true);
    if (state.kind === "found") onInsert(state.key);
  }

  const fieldId = `${id}-key`;
  const statusId = `${id}-status`;
  const problem =
    state.kind === "notAKey" && tried
      ? p.notAKey
      : state.kind === "notFound"
        ? p.notFound(state.key)
        : state.kind === "unreachable"
          ? p.unreachable
          : undefined;
  return (
    <Dialog title={p.title} onClose={onClose} data-issue-picker="">
      <form onSubmit={submit} className="space-y-3" noValidate>
        <Labelled id={fieldId} label={p.field} hint={p.hint} error={problem}>
          <Input
            id={fieldId}
            value={text}
            autoComplete="off"
            spellCheck={false}
            invalid={Boolean(problem)}
            aria-invalid={problem ? true : undefined}
            aria-describedby={[describedBy(fieldId, p.hint, problem), statusId].filter(Boolean).join(" ")}
            onChange={(event) => {
              setText(event.target.value);
              setTried(false);
            }}
          />
        </Labelled>
        <div id={statusId} role="status" className="min-h-6 text-sm text-ink-muted" data-picker-state={state.kind}>
          {state.kind === "looking" && p.looking}
          {state.kind === "connect" && (
            <span>
              {p.connect}{" "}
              <a href={`${PROFILE_PATH}#${ARMATURE_SECTION_ID}`} className="underline">
                {p.connectLink}
              </a>
            </span>
          )}
          {state.kind === "found" && (
            <span className="inline-flex max-w-full items-center gap-1 text-ink" data-picked={state.key}>
              <TypeIcon icon={state.type.icon} name={state.type.name} />
              <span className="sr-only">{p.found(state.key, state.summary)}</span>
              <span aria-hidden="true" className="font-medium">
                {state.key}
              </span>
              <span aria-hidden="true" className="min-w-0 truncate">
                {state.summary}
              </span>
              <StatusLozenge name={state.status.name} category={state.status.category} />
            </span>
          )}
        </div>
        <div className="flex justify-end gap-2 pt-1">
          <Button type="button" variant="secondary" onClick={onClose}>
            {p.cancel}
          </Button>
          <Button type="submit" disabled={state.kind === "connect"} data-action="insert-issue">
            {p.insert}
          </Button>
        </div>
      </form>
    </Dialog>
  );
}
