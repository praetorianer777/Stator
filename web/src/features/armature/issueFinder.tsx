import { useCallback, useMemo } from "react";
import { lacks, useArmatureAccount, useArmatureFind, useArmatureIssue, useArmatureLookup, type ArmatureIssue } from "@/api/armature";
import { useMe } from "@/api/auth";
import { cx } from "@/components/ui";
import { ARMATURE_FIND_DEBOUNCE_MS, ARMATURE_FIND_MAX_LENGTH, ARMATURE_FIND_MIN_LENGTH, ARMATURE_PICKER_DEBOUNCE_MS } from "@/config";
import { t } from "@/i18n";
import { useDebounced } from "@/lib/debounce";
import { StatusLozenge, TypeIcon } from "./IssueChip";
import { keyFromIssueUrl, normalizeKey } from "./issueKeys";
import { outdatedSentence } from "./outdated";
import { ConnectArmatureLink } from "./connectLink";
import { recentIssueKeys, rememberIssue } from "./recentIssues";

/** What a finder has for what was typed, as one state its field reads. */
export type FinderState =
  | { kind: "outdated" }
  | { kind: "connect" }
  | { kind: "recent" }
  | { kind: "short" }
  | { kind: "looking" }
  | { kind: "unreachable" }
  | { kind: "notFound"; key: string }
  | { kind: "noMatch" }
  | { kind: "found" };

/** The key a person typed or pasted: a key in any case, or an issue address of the connected Armature. */
export function pickedKey(text: string, baseUrl: string | null): string | null {
  return normalizeKey(text) ?? keyFromIssueUrl(text, baseUrl);
}

export interface IssueFinder {
  state: FinderState;
  /** The issues to offer: those found, or the recent ones while nothing is typed. */
  issues: ArmatureIssue[];
  /** Puts an inserted issue first among the person's recent ones. */
  remember: (key: string) => void;
}

const NONE: ArmatureIssue[] = [];

/**
 * Finds issues for what a person types: a key or an issue address straight
 * away, else words of the summary or description, debounced, so a field
 * has one request in flight. While nothing is typed it offers the issues the
 * person inserted last in this browser.
 */
export function useIssueFinder(text: string, enabled = true): IssueFinder {
  const account = useArmatureAccount();
  const me = useMe();
  const userId = me.data?.user.id;
  const baseUrl = account.data?.configured ? (account.data.baseUrl ?? null) : null;
  const connected = Boolean(account.data?.connected && account.data.status !== "rejected");
  const outdated = lacks(account.data, "issues");
  const usable = enabled && connected && !outdated;

  const typed = text.trim();
  const key = pickedKey(typed, baseUrl);
  const settledKey = useDebounced(key, ARMATURE_PICKER_DEBOUNCE_MS);
  const exact = useArmatureIssue(settledKey ?? "", usable && key !== null && settledKey === key);

  const words = key === null && typed.length >= ARMATURE_FIND_MIN_LENGTH ? typed.slice(0, ARMATURE_FIND_MAX_LENGTH) : "";
  const settledWords = useDebounced(words, ARMATURE_FIND_DEBOUNCE_MS);
  const found = useArmatureFind(settledWords, usable && words !== "" && settledWords === words);

  // Read once per finder and person: what is inserted while it is open joins the list next time.
  const keys = useMemo(() => recentIssueKeys(userId), [userId]);
  const recent = useArmatureLookup(keys, usable && typed === "");

  let state: FinderState;
  if (outdated) state = { kind: "outdated" };
  else if (account.isPending) state = { kind: "looking" };
  else if (!connected) state = { kind: "connect" };
  else if (typed === "") {
    state = { kind: "recent" };
  } else if (key !== null) {
    if (settledKey !== key || exact.isPending) state = { kind: "looking" };
    else if (exact.isError || exact.data?.status === "unreachable") state = { kind: "unreachable" };
    else if (exact.data?.status === "outdated") state = { kind: "outdated" };
    else if (exact.data?.status !== "ok") state = { kind: "connect" };
    else if (!exact.data.issue) state = { kind: "notFound", key };
    else state = { kind: "found" };
  } else if (words === "") state = { kind: "short" };
  else if (settledWords !== words || found.isPending) state = { kind: "looking" };
  else if (found.isError || found.data?.status === "unreachable") state = { kind: "unreachable" };
  else if (found.data?.status === "outdated") state = { kind: "outdated" };
  else if (found.data?.status !== "ok") state = { kind: "connect" };
  else if (found.data.issues.length === 0) state = { kind: "noMatch" };
  else state = { kind: "found" };

  const exactIssue = state.kind === "found" && key !== null ? (exact.data?.issue ?? null) : null;
  const foundIssues = state.kind === "found" && key === null ? found.data?.issues : undefined;
  const recentIssues = state.kind === "recent" && recent.data?.status === "ok" ? recent.data.issues : undefined;
  const issues = useMemo(() => {
    if (exactIssue) return [exactIssue];
    if (foundIssues) return foundIssues;
    if (recentIssues) return recentIssues.flatMap((each) => (each.issue ? [each.issue] : []));
    return NONE;
  }, [exactIssue, foundIssues, recentIssues]);

  const remember = useCallback((picked: string) => rememberIssue(userId, picked), [userId]);
  return { state, issues, remember };
}

/** The sentence a finder's status line says, or null when its options say enough. */
export function FinderStatus({ state, count }: { state: FinderState; count: number }) {
  const f = t.armature.find;
  switch (state.kind) {
    case "outdated":
      return <>{outdatedSentence("issues")}</>;
    case "connect":
      return (
        <span>
          {f.connect} <ConnectArmatureLink className="underline">{t.armature.picker.connectLink}</ConnectArmatureLink>
        </span>
      );
    case "recent":
      return <>{count > 0 ? f.recent : f.hint}</>;
    case "short":
      return <>{f.short(ARMATURE_FIND_MIN_LENGTH)}</>;
    case "looking":
      return <>{f.looking}</>;
    case "unreachable":
      return <>{t.armature.picker.unreachable}</>;
    case "notFound":
      return <>{t.armature.picker.notFound(state.key)}</>;
    case "noMatch":
      return <>{f.noMatch}</>;
    case "found":
      return <>{f.found(count)}</>;
  }
}

/** The issues a finder offers, as options: type, key, summary, status and project. */
export function IssueOptions({
  id,
  label,
  issues,
  active,
  onPick,
  onHover,
  listRef,
  className,
}: {
  id: string;
  label: string;
  issues: readonly ArmatureIssue[];
  active: number;
  onPick: (issue: ArmatureIssue) => void;
  onHover: (i: number) => void;
  listRef?: (list: HTMLElement | null) => void;
  className?: string;
}) {
  return (
    <div ref={listRef} id={id} role="listbox" aria-label={label} className={className} data-issue-options>
      {issues.map((issue, i) => (
        <div
          key={issue.key}
          id={`${id}-${i}`}
          role="option"
          aria-selected={i === active}
          tabIndex={-1}
          data-issue-option={issue.key}
          // The field or the editor keeps focus, so a click picks without taking it.
          onMouseDown={(e) => {
            e.preventDefault();
            onPick(issue);
          }}
          onMouseEnter={() => onHover(i)}
          className={cx(
            "flex cursor-pointer items-center gap-2 rounded-control px-2 py-1 text-sm",
            i === active ? "bg-surface-raised text-ink" : "text-ink-muted",
          )}
        >
          <TypeIcon icon={issue.type.icon} name={issue.type.name} />
          <span className="shrink-0 font-medium text-ink">{issue.key}</span>
          <span className="min-w-0 flex-1 truncate">{issue.summary}</span>
          <StatusLozenge name={issue.status.name} category={issue.status.category} />
          <span className="shrink-0 text-xs text-ink-muted">
            <span className="sr-only">{t.armature.find.project} </span>
            {issue.projectKey}
          </span>
        </div>
      ))}
    </div>
  );
}
