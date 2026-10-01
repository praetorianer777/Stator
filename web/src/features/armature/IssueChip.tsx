import { createContext, useContext, useEffect, useId, useMemo, useRef, useState, type CSSProperties, type ReactNode, type RefObject } from "react";
import { useArmatureAccount, useArmatureIssue, useArmatureIssues, type ArmatureAccount, type ArmatureIssue, type ArmatureIssues } from "@/api/armature";
import { cx } from "@/components/ui";
import { useAnchored, useEscape } from "@/components/ui/overlay";
import { Icon, type IconName } from "@/components/icons";
import { ARMATURE_CARD_DELAY_MS, ARMATURE_SECTION_ID, PROFILE_PATH } from "@/config";
import { t } from "@/i18n";
import { issueUrl, lookupBatches } from "./issueKeys";

/**
 * What a chip can say about its issue to this viewer: only its key, the key
 * and the way to connect, the issue itself, or that it is not theirs to see.
 */
export type ChipState =
  | { kind: "plain" }
  | { kind: "connect"; href: string }
  | { kind: "loading"; href: string }
  | { kind: "unreachable"; href: string }
  | { kind: "hidden"; href: string }
  | { kind: "issue"; issue: ArmatureIssue };

type ChipSource = (key: string) => ChipState;

const plain: ChipSource = () => ({ kind: "plain" });
const ChipContext = createContext<ChipSource>(plain);

/** What the chip of key shows; without a provider, the key alone. */
export function useChip(key: string): ChipState {
  return useContext(ChipContext)(key);
}

/** Decides a chip's state from the viewer's account and what the lookup found. */
export function chipState(key: string, account: ArmatureAccount | undefined, lookup: ArmatureIssues): ChipState {
  if (!account?.configured || !account.baseUrl) return { kind: "plain" };
  const href = issueUrl(account.baseUrl, key);
  if (!account.connected || account.status === "rejected") return { kind: "connect", href };
  switch (lookup.status) {
    case "not_configured":
      return { kind: "plain" };
    case "not_connected":
    case "rejected":
      return { kind: "connect", href };
    case "unreachable":
      return { kind: "unreachable", href };
  }
  if (!lookup.issues.has(key)) return { kind: "loading", href };
  const issue = lookup.issues.get(key);
  return issue ? { kind: "issue", issue } : { kind: "hidden", href };
}

/**
 * Looks the issues a view names up once, in batches the API takes, and tells
 * every chip below what to show. Answers stay while a changed set is asked.
 */
export function ArmatureIssuesProvider({ keys, children }: { keys: readonly string[]; children: ReactNode }) {
  const account = useArmatureAccount();
  // The joined keys say when the set changed; the array is new on every render.
  const signature = keys.join(" ");
  const batches = useMemo(() => lookupBatches(signature ? signature.split(" ") : []), [signature]);
  const asks = Boolean(account.data?.connected && account.data.status !== "rejected");
  const lookup = useArmatureIssues(batches, asks && batches.length > 0);
  const known = useRef<ArmatureIssues>({ status: undefined, issues: new Map() });
  const merged = useMemo(() => {
    if (lookup.status === undefined) return known.current;
    const issues = new Map(lookup.status === "ok" ? known.current.issues : []);
    for (const [key, issue] of lookup.issues) issues.set(key, issue);
    known.current = { status: lookup.status, issues };
    return known.current;
  }, [lookup]);
  const source = useMemo<ChipSource>(() => (key) => chipState(key, account.data, merged), [account.data, merged]);
  return <ChipContext value={source}>{children}</ChipContext>;
}

// Armature's icon names for its issue types; an unknown one is a task, as
// Armature draws it.
const TYPE_ICONS: Record<string, IconName> = { bug: "Bug", story: "Story", epic: "Epic", initiative: "Initiative", subtask: "Subtask", task: "Task" };

const TYPE_INK: Record<string, string> = {
  bug: "text-danger",
  story: "text-success",
  epic: "text-epic",
  initiative: "text-warning",
};

const STATUS_STYLES: Record<string, string> = {
  todo: "bg-status-todo-subtle",
  in_progress: "bg-status-progress-subtle",
  done: "bg-status-done-subtle",
};

/** The issue type's glyph in Armature's shape and colour; an unknown icon draws a task. */
export function TypeIcon({ icon, name }: { icon: string; name: string }) {
  const Glyph = Icon[TYPE_ICONS[icon] ?? "Task"];
  return <Glyph label={name} size={14} className={cx("shrink-0", TYPE_INK[icon] ?? "text-ink-muted")} />;
}

/** The status as a lozenge, coloured by its category: every workflow's states are still to do, in progress or done. */
export function StatusLozenge({ name, category }: { name: string; category: string }) {
  return (
    <span
      className={cx(
        "rounded px-1 py-px text-2xs font-semibold tracking-wide whitespace-nowrap text-ink uppercase",
        STATUS_STYLES[category] ?? STATUS_STYLES.todo,
      )}
      data-status-category={category}
    >
      <span className="sr-only">{t.armature.chip.status} </span>
      {name}
    </span>
  );
}

const CHIP =
  "inline-flex max-w-full items-baseline gap-1 rounded-control border border-border bg-surface-raised px-1.5 align-baseline text-[0.9em] leading-snug";

/**
 * An Armature issue in running text, drawn from its key alone. Without links
 * in the editor, where a click selects it rather than following it.
 */
export function IssueChip({ issueKey, links = true }: { issueKey: string; links?: boolean }) {
  const state = useChip(issueKey);
  const common = { "data-armature-issue": issueKey, "data-state": state.kind };
  if (state.kind === "plain") {
    return (
      <span {...common} className={CHIP}>
        <span className="font-medium">{issueKey}</span>
      </span>
    );
  }
  if (state.kind !== "issue") {
    const key = links ? (
      <a href={state.href} target="_blank" rel="noopener noreferrer" className="font-medium">
        {issueKey}
      </a>
    ) : (
      <span className="font-medium">{issueKey}</span>
    );
    return (
      <span {...common} className={CHIP}>
        {key}
        {state.kind === "connect" && <ConnectHint links={links} />}
        {state.kind === "hidden" && <span className="text-xs text-ink-muted">{t.armature.chip.notAvailable}</span>}
        {state.kind === "unreachable" && <span className="text-xs text-ink-muted">{t.armature.chip.unreachable}</span>}
      </span>
    );
  }
  return <IssueLink issue={state.issue} issueKey={issueKey} links={links} />;
}

function ConnectHint({ links }: { links: boolean }) {
  if (!links) return <span className="text-xs text-ink-muted">{t.armature.chip.connectShort}</span>;
  return (
    <a href={`${PROFILE_PATH}#${ARMATURE_SECTION_ID}`} className="text-xs text-ink-muted underline" data-action="connect-armature-hint">
      {t.armature.chip.connectShort}
      <span className="sr-only">{t.armature.chip.connectRest}</span>
    </a>
  );
}

function IssueLink({ issue, issueKey, links }: { issue: ArmatureIssue; issueKey: string; links: boolean }) {
  const [shown, setShown] = useState(false);
  const timer = useRef<number | undefined>(undefined);
  const anchorRef = useRef<HTMLElement>(null);
  const cardRef = useRef<HTMLSpanElement>(null);
  const id = useId();
  const place = useAnchored(shown, anchorRef, cardRef, { side: "bottom" });
  useEffect(() => () => window.clearTimeout(timer.current), []);
  const hide = () => {
    window.clearTimeout(timer.current);
    setShown(false);
  };
  useEscape(shown, hide);
  const show = () => {
    window.clearTimeout(timer.current);
    timer.current = window.setTimeout(() => setShown(true), ARMATURE_CARD_DELAY_MS);
  };
  const body = (
    <>
      <TypeIcon icon={issue.type.icon} name={issue.type.name} />
      {/* An issue that moved shows the key it has now; the page keeps the one it was named by. */}
      <span className="font-medium">{issue.key}</span>
      <span className="min-w-0 truncate">{issue.summary}</span>
      <StatusLozenge name={issue.status.name} category={issue.status.category} />
    </>
  );
  const props = {
    className: cx(CHIP, "text-ink no-underline"),
    "data-armature-issue": issueKey,
    "data-state": "issue",
    onPointerEnter: show,
    onPointerLeave: hide,
    onFocus: show,
    onBlur: hide,
    "aria-describedby": shown ? id : undefined,
  };
  // The card is drawn beside the link, not in a portal, so it stays inside the
  // page's landmarks, and not inside the link, whose name it would join.
  return (
    <span className="inline">
      {links ? (
        <a {...props} ref={anchorRef as RefObject<HTMLAnchorElement>} href={issue.url} target="_blank" rel="noopener noreferrer">
          {body}
        </a>
      ) : (
        <span {...props} ref={anchorRef as RefObject<HTMLSpanElement>}>
          {body}
        </span>
      )}
      {shown && <IssueCard id={id} cardRef={cardRef} style={place} issueKey={issueKey} fallback={issue} />}
    </span>
  );
}

const when = new Intl.DateTimeFormat(undefined, { dateStyle: "medium", timeStyle: "short" });

/** The issue's details beside its chip; it explains the chip and holds nothing to press. */
function IssueCard({
  id,
  cardRef,
  style,
  issueKey,
  fallback,
}: {
  id: string;
  cardRef: RefObject<HTMLSpanElement | null>;
  style: CSSProperties;
  issueKey: string;
  fallback: ArmatureIssue;
}) {
  const fresh = useArmatureIssue(issueKey, true);
  const issue = fresh.data?.status === "ok" && fresh.data.issue ? fresh.data.issue : fallback;
  const c = t.armature.chip;
  // Spans throughout: the chip sits in running text, where a block may not.
  const row = (label: string, value: ReactNode) => (
    <span className="flex items-center gap-3">
      <span className="w-20 shrink-0 text-ink-muted">{label}</span>
      <span className="inline-flex min-w-0 items-center gap-1">{value}</span>
    </span>
  );
  return (
    <span
      ref={cardRef}
      id={id}
      role="tooltip"
      style={style}
      className="fixed z-40 block w-72 space-y-2 rounded-card border border-border bg-surface-overlay p-3 text-sm whitespace-normal text-ink shadow-2"
      data-armature-card
    >
      <span className="block font-medium">{issue.summary}</span>
      <span className="block space-y-1 text-xs">
        {row(
          c.type,
          <>
            <TypeIcon icon={issue.type.icon} name="" />
            {issue.type.name}
          </>,
        )}
        {row(c.status, <StatusLozenge name={issue.status.name} category={issue.status.category} />)}
        {row(c.priority, c.priorities[issue.priority] ?? issue.priority)}
        {row(c.assignee, issue.assignee?.name ?? c.unassigned)}
        {row(c.updated, when.format(new Date(issue.updatedAt)))}
      </span>
    </span>
  );
}
