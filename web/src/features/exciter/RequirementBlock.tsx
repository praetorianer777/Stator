import type { ReactNode } from "react";
import { useExciterRequirement, type ExciterRequirement } from "@/api/exciter";
import { Icon } from "@/components/icons";
import { cx } from "@/components/ui";
import { EXCITER_FEATURE_CAPABILITIES } from "@/config";
import { t } from "@/i18n";
import { ExciterDocument, hasContent } from "./ExciterDocument";
import { RequirementChip, RequirementRevision, RequirementStatusLozenge, useRequirementChip } from "./RequirementChip";

/**
 * One Exciter requirement with its statement and rationale, drawn from its
 * key for whoever views it. Without a token or without access it is the key
 * and the hint, as a chip.
 */
export function RequirementBlock({ requirementKey, links = true }: { requirementKey: string; links?: boolean }) {
  const b = t.exciter.block;
  const state = useRequirementChip(requirementKey);
  const detail = useExciterRequirement(requirementKey, state.kind === "requirement");
  const frame = {
    role: "group",
    "aria-label": b.label(requirementKey),
    "data-exciter-requirement-block": requirementKey,
  } as const;
  if (state.kind !== "requirement") {
    return (
      <div {...frame} data-state={state.kind} className="doc-block space-y-1">
        <RequirementChip requirementKey={requirementKey} links={links} />
        {state.kind === "outdated" && (
          <p className="text-sm text-ink-muted">
            {t.exciter.capabilities.outdated(t.exciter.capabilities.features.requirements, EXCITER_FEATURE_CAPABILITIES.requirements)}
          </p>
        )}
        {state.kind === "hidden" && <p className="text-sm text-ink-muted">{t.exciter.chip.hiddenSaid(requirementKey)}</p>}
      </div>
    );
  }
  const found = detail.data?.status === "ok" ? detail.data.requirement : undefined;
  const requirement: ExciterRequirement = found ?? state.requirement;
  const trashed = Boolean(requirement.trashedAt);
  let texts: ReactNode;
  if (detail.isPending) texts = <p className="text-sm text-ink-muted">{b.loading}</p>;
  else if (!found) texts = <p className="text-sm text-ink-muted">{b.failed}</p>;
  else {
    texts = (
      <>
        <Section label={b.statement} name="statement">
          {hasContent(found.statement) ? <ExciterDocument doc={found.statement} links={links} /> : <p className="text-ink-muted">{b.none}</p>}
        </Section>
        {hasContent(found.rationale) && (
          <Section label={b.rationale} name="rationale">
            <ExciterDocument doc={found.rationale} links={links} />
          </Section>
        )}
      </>
    );
  }
  return (
    <div {...frame} data-state={trashed ? "trashed" : "requirement"} className="doc-block space-y-2">
      <div className="flex flex-wrap items-center gap-2">
        {/* A requirement that moved shows the key it has now; the page keeps the one it was named by. */}
        <span className={cx("font-medium", trashed && "line-through")} data-requirement-key>
          {requirement.key}
        </span>
        <RequirementStatusLozenge status={requirement.status} />
        <RequirementRevision revision={requirement.revision} />
        {trashed && <span className="text-xs text-ink-muted">{t.exciter.chip.trashed}</span>}
      </div>
      <div className="font-medium text-ink" data-requirement-title>
        {requirement.title}
      </div>
      {texts}
      {links && (
        <a href={requirement.url} target="_blank" rel="noopener noreferrer" className="inline-flex items-center gap-1 text-sm" data-action="open-in-exciter">
          {b.open}
          <span className="sr-only">{b.openRest(requirement.key)}</span>
          <Icon.External />
        </a>
      )}
    </div>
  );
}

function Section({ label, name, children }: { label: string; name: string; children: ReactNode }) {
  return (
    <section className="space-y-1 text-sm" data-requirement-text={name} aria-label={label}>
      <div className="text-xs font-semibold tracking-wide text-ink-muted uppercase" aria-hidden="true">
        {label}
      </div>
      {children}
    </section>
  );
}
