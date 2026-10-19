import { createContext, useContext, useMemo, useRef, type ReactNode } from "react";
import { exciterLacks, useExciterAccount, useExciterRequirements, type ExciterAccount, type ExciterRequirement, type ExciterRequirements } from "@/api/exciter";
import { cx } from "@/components/ui";
import { EXCITER_FEATURE_CAPABILITIES } from "@/config";
import { ConnectExciterLink } from "./connectLink";
import { t } from "@/i18n";
import { requirementBatches, requirementUrl } from "./requirementKeys";

/**
 * What a chip can say about its requirement to this viewer: only its key, the
 * key and the way to connect, the requirement itself, or that it is not theirs to see.
 */
export type RequirementChipState =
  | { kind: "plain" }
  | { kind: "connect"; href: string; refused?: boolean }
  | { kind: "loading"; href: string }
  | { kind: "unreachable"; href: string }
  | { kind: "outdated"; href: string }
  | { kind: "hidden"; href: string }
  | { kind: "requirement"; requirement: ExciterRequirement };

type ChipSource = (key: string) => RequirementChipState;

const plain: ChipSource = () => ({ kind: "plain" });
const ChipContext = createContext<ChipSource>(plain);

/** What the chip of key shows; without a provider, the key alone. */
export function useRequirementChip(key: string): RequirementChipState {
  return useContext(ChipContext)(key);
}

/** Decides a chip's state from the viewer's account and what the lookup found. */
export function requirementChipState(key: string, account: ExciterAccount | undefined, lookup: ExciterRequirements): RequirementChipState {
  if (!account?.configured || !account.baseUrl) return { kind: "plain" };
  const href = requirementUrl(account.baseUrl, key);
  if (!account.connected || account.status === "rejected") return { kind: "connect", href, refused: account.status === "rejected" };
  if (exciterLacks(account, "requirements")) return { kind: "outdated", href };
  switch (lookup.status) {
    case "not_configured":
      return { kind: "plain" };
    case "not_connected":
      return { kind: "connect", href };
    case "rejected":
      return { kind: "connect", href, refused: true };
    case "unreachable":
      return { kind: "unreachable", href };
    case "outdated":
      return { kind: "outdated", href };
  }
  if (!lookup.requirements.has(key)) return { kind: "loading", href };
  const requirement = lookup.requirements.get(key);
  return requirement ? { kind: "requirement", requirement } : { kind: "hidden", href };
}

/**
 * Looks the requirements a view names up once, in batches the API takes, and
 * tells every chip below what to show. Answers stay while a changed set is asked.
 */
export function ExciterRequirementsProvider({ keys, children }: { keys: readonly string[]; children: ReactNode }) {
  const account = useExciterAccount();
  const signature = keys.join(" ");
  const batches = useMemo(() => requirementBatches(signature ? signature.split(" ") : []), [signature]);
  const asks = Boolean(account.data?.connected && account.data.status !== "rejected" && !exciterLacks(account.data, "requirements"));
  const lookup = useExciterRequirements(batches, asks && batches.length > 0);
  const known = useRef<ExciterRequirements>({ status: undefined, requirements: new Map() });
  const merged = useMemo(() => {
    if (lookup.status === undefined) return known.current;
    const requirements = new Map(lookup.status === "ok" ? known.current.requirements : []);
    for (const [key, requirement] of lookup.requirements) requirements.set(key, requirement);
    known.current = { status: lookup.status, requirements };
    return known.current;
  }, [lookup]);
  const source = useMemo<ChipSource>(() => (key) => requirementChipState(key, account.data, merged), [account.data, merged]);
  return <ChipContext value={source}>{children}</ChipContext>;
}

const STATUS_STYLES: Record<string, string> = {
  draft: "bg-status-todo-subtle",
  review: "bg-status-progress-subtle",
  approved: "bg-status-done-subtle",
  done: "bg-status-done-subtle",
  closed: "bg-status-todo-subtle",
};

const CHIP =
  "inline-flex max-w-full items-baseline gap-1 rounded-control border border-border bg-surface-raised px-1.5 align-baseline text-[0.9em] leading-snug";

/**
 * An Exciter requirement in running text, drawn from its key alone. Without
 * links in the editor, where a click selects it rather than following it.
 */
export function RequirementChip({ requirementKey, links = true }: { requirementKey: string; links?: boolean }) {
  const state = useRequirementChip(requirementKey);
  const common = { "data-exciter-requirement": requirementKey, "data-state": state.kind };
  const c = t.exciter.chip;
  if (state.kind === "plain") {
    return (
      <span {...common} className={CHIP}>
        <span className="font-medium">{requirementKey}</span>
      </span>
    );
  }
  if (state.kind === "requirement") return <RequirementLink requirement={state.requirement} requirementKey={requirementKey} links={links} />;
  const key = links ? (
    <a href={state.href} target="_blank" rel="noopener noreferrer" className="font-medium">
      {requirementKey}
    </a>
  ) : (
    <span className="font-medium">{requirementKey}</span>
  );
  return (
    <span {...common} className={CHIP}>
      {key}
      {state.kind === "connect" && <ConnectHint links={links} refused={state.refused ?? false} />}
      {state.kind === "unreachable" && <span className="text-xs text-ink-muted">{c.unreachable}</span>}
      {state.kind === "hidden" && (
        <span className="text-xs text-ink-muted" title={c.hiddenSaid(requirementKey)}>
          {c.hidden}
        </span>
      )}
      {state.kind === "outdated" && (
        <span
          className="text-xs text-ink-muted"
          title={t.exciter.capabilities.outdated(t.exciter.capabilities.features.requirements, EXCITER_FEATURE_CAPABILITIES.requirements)}
          data-exciter-outdated="requirements"
        >
          {t.exciter.capabilities.short}
        </span>
      )}
    </span>
  );
}

function ConnectHint({ links, refused }: { links: boolean; refused: boolean }) {
  const c = t.exciter.chip;
  const short = refused ? c.replaceShort : c.connectShort;
  if (!links) return <span className="text-xs text-ink-muted">{short}</span>;
  return (
    <ConnectExciterLink className="text-xs text-ink-muted underline">
      {short}
      <span className="sr-only">{c.connectRest}</span>
    </ConnectExciterLink>
  );
}

function RequirementLink({ requirement, requirementKey, links }: { requirement: ExciterRequirement; requirementKey: string; links: boolean }) {
  const c = t.exciter.chip;
  const trashed = Boolean(requirement.trashedAt);
  const body = (
    <>
      {/* A requirement that moved shows the key it has now; the page keeps the one it was named by. */}
      <span className={cx("font-medium", trashed && "line-through")}>{requirement.key}</span>
      <span className="min-w-0 truncate">{requirement.title}</span>
      {requirement.status && (
        <span
          className={cx(
            "rounded px-1 py-px text-2xs font-semibold tracking-wide whitespace-nowrap text-ink uppercase",
            STATUS_STYLES[requirement.status.category] ?? STATUS_STYLES.draft,
          )}
          data-status-category={requirement.status.category}
        >
          <span className="sr-only">{c.status} </span>
          {requirement.status.name}
        </span>
      )}
      <span className="text-xs whitespace-nowrap text-ink-muted" title={c.revision(requirement.revision)} data-revision={requirement.revision}>
        <span aria-hidden="true">{c.revisionShort(requirement.revision)}</span>
        <span className="sr-only">{c.revision(requirement.revision)}</span>
      </span>
      {trashed && <span className="text-xs text-ink-muted">{c.trashed}</span>}
    </>
  );
  const props = {
    className: cx(CHIP, "text-ink no-underline"),
    "data-exciter-requirement": requirementKey,
    "data-state": trashed ? "trashed" : "requirement",
  };
  return links ? (
    <a {...props} href={requirement.url} target="_blank" rel="noopener noreferrer">
      {body}
    </a>
  ) : (
    <span {...props}>{body}</span>
  );
}
