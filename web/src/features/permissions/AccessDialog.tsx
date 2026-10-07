import { useId, useState, type ReactNode } from "react";
import { subjectKey, useInspectAccess, type AccessReport, type AccessRight, type AccessStep, type SpaceGrant, type Subject } from "@/api/permissions";
import { Icon } from "@/components/icons";
import { Dialog, ErrorBanner, Skeleton, Tag, cx } from "@/components/ui";
import { t } from "@/i18n";
import { SubjectList } from "./SubjectList";
import { SubjectGlyph, SubjectPicker } from "./SubjectPicker";

/** The first step a right does not meet, which is why it is refused; -1 when it is allowed. */
export function decidingStep(right: AccessRight): number {
  return right.steps.findIndex((step) => !step.passed);
}

/** One step as a sentence about the person checked. */
export function stepSentence(step: AccessStep, report: Pick<AccessReport, "role" | "roleSource">): string {
  const permission = step.permission ? t.permissions.spaceNames[step.permission] : "";
  const title = step.page?.title ?? "";
  const list = step.list === "view" ? "view" : "edit";
  switch (step.kind) {
    case "orgAdmin": {
      const role = t.access.orgAdmin(report.role === "owner");
      return report.roleSource === "oidc" ? `${role} ${t.access.fromProvider}` : role;
    }
    case "use":
      return step.passed ? t.access.useYes : t.access.useNo;
    case "space":
      return step.passed ? t.access.spaceYes(permission) : t.access.spaceNo(permission);
    case "unpublished":
      return t.access.unpublished(title);
    case "list":
      if (step.bypassed) return t.access.listBypassed(list, title);
      return step.passed ? t.access.listYes(list, title) : t.access.listNo(list, title);
    case "grant":
      return t.access.grantYes(title);
    case "view":
      return step.passed ? t.access.viewYes : t.access.viewNo;
    case "published":
      return t.access.publishedNo;
    case "home":
      return t.access.homeNo;
    case "archived":
      return step.page ? t.access.archivedPage(step.page.title) : t.access.archivedSpace;
  }
}

/**
 * Pick a person and see what they may do on a page: each right, and the
 * grants and restrictions behind it, the one that decides a no marked.
 */
export function AccessDialog({ pageId, pageTitle, onClose }: { pageId: string; pageTitle: string; onClose: () => void }) {
  const [person, setPerson] = useState<Subject>();
  return (
    <Dialog title={t.access.title(pageTitle)} wide onClose={onClose} data-access-dialog={pageId}>
      <div className="space-y-4">
        <p className="text-sm text-ink-muted">{t.access.intro}</p>
        <SubjectPicker label={t.access.pickerLabel} peopleOnly onPick={setPerson} />
        {person?.id && <Report pageId={pageId} userId={person.id} />}
      </div>
    </Dialog>
  );
}

function Report({ pageId, userId }: { pageId: string; userId: string }) {
  const { data, error, refetch } = useInspectAccess(pageId, userId);
  const headingId = useId();
  if (error) return <ErrorBanner onRetry={() => void refetch()}>{error.message}</ErrorBanner>;
  if (!data)
    return (
      <div role="status" aria-label={t.access.loading}>
        <Skeleton />
      </div>
    );
  return (
    <section className="space-y-3" aria-labelledby={headingId} data-access-report={data.person.id}>
      <h3 id={headingId} className="text-sm font-semibold text-ink">
        {t.access.heading(data.person.name || data.person.email)}
      </h3>
      <ul className="space-y-3">
        {data.rights.map((right) => (
          <RightCard key={right.right} right={right} report={data} />
        ))}
      </ul>
    </section>
  );
}

function RightCard({ right, report }: { right: AccessRight; report: AccessReport }) {
  const deciding = decidingStep(right);
  const name = t.access.rights[right.right];
  return (
    <li className="space-y-2 rounded-control border border-border p-3" data-access-right={right.right} data-allowed={right.allowed}>
      <h4 className="flex items-center gap-2 text-sm font-semibold text-ink">
        {right.allowed ? <Icon.Check className="text-success" /> : <Icon.X className="text-danger" />}
        <span>{name}</span>
        <Tag className={right.allowed ? "text-success" : "text-danger"}>{right.allowed ? t.access.allowed : t.access.denied}</Tag>
      </h4>
      <ol className="space-y-2">
        {right.steps.map((step, i) => (
          <Step key={`${step.kind}-${step.page?.id ?? ""}-${step.list ?? ""}-${step.permission ?? ""}`} step={step} report={report} deciding={i === deciding} />
        ))}
      </ol>
    </li>
  );
}

function Step({ step, report, deciding }: { step: AccessStep; report: AccessReport; deciding: boolean }) {
  const passed = step.passed;
  return (
    <li
      className={cx("flex gap-2 text-sm", deciding ? "rounded-control bg-surface-raised p-2 text-ink" : "text-ink-muted")}
      data-access-step={step.kind}
      data-passed={passed}
      data-decides={deciding || undefined}
    >
      <span className="mt-0.5 shrink-0" aria-hidden="true">
        {passed ? <Icon.Check className="text-success" /> : <Icon.X className="text-danger" />}
      </span>
      <div className="min-w-0 flex-1 space-y-1.5">
        <p>
          <span className="sr-only">{passed ? t.access.passed : t.access.failed}: </span>
          {stepSentence(step, report)}
          {deciding && <strong className="ml-1.5 font-semibold text-danger">{t.access.decides}</strong>}
        </p>
        {step.grants.length > 0 && (
          <Named label={t.access.grantedTo}>
            <GrantList grants={step.grants} />
          </Named>
        )}
        {step.via.length > 0 && (
          <Named label={step.kind === "use" ? t.access.grantedTo : t.access.namedAs}>
            <SubjectList subjects={step.via} empty={t.access.nobody} />
          </Named>
        )}
        {step.kind === "list" && !passed && (
          <Named label={t.access.listNames}>
            <SubjectList subjects={step.listed} empty={t.access.nobody} />
          </Named>
        )}
      </div>
    </li>
  );
}

function Named({ label, children }: { label: string; children: ReactNode }) {
  return (
    <div className="flex flex-wrap items-center gap-1.5">
      <span className="text-xs text-ink-subtle">{label}</span>
      {children}
    </div>
  );
}

/** The space's grants that bring the person in, each with the permissions that count. */
function GrantList({ grants }: { grants: SpaceGrant[] }) {
  return (
    <ul className="flex flex-wrap gap-1.5" data-grant-list>
      {grants.map((grant) => (
        <li
          key={subjectKey(grant.subject)}
          className="inline-flex h-7 items-center gap-1.5 rounded-control border border-border bg-surface-raised px-2 text-sm text-ink"
          data-grant={grant.subject.name}
        >
          <SubjectGlyph type={grant.subject.type} />
          <span>{grant.subject.type === "everyone" ? t.permissions.everyone : grant.subject.name}</span>
          <span className="text-xs text-ink-subtle">{grant.permissions.map((p) => t.permissions.spaceNames[p]).join(", ")}</span>
        </li>
      ))}
    </ul>
  );
}
