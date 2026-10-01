import { localDateFormat } from "@/lib/format";
import type { ReactNode } from "react";
import type { ArmatureIssue } from "@/api/armature";
import { Icon } from "@/components/icons";
import { t } from "@/i18n";
import { IssueChip, StatusLozenge, TypeIcon, useChip } from "./IssueChip";

const changedAt = localDateFormat({ dateStyle: "medium", timeStyle: "short" });
// Armature keeps a due date as a day at midnight UTC; read in the viewer's
// zone it could fall on the day before.
const dueOn = localDateFormat({ dateStyle: "medium", timeZone: "UTC" });

/** A due date as the day Armature names. */
export function formatDue(dueDate: string | null | undefined): string | null {
  return dueDate ? dueOn.format(new Date(dueDate)) : null;
}

/**
 * One Armature issue as a card, drawn from its key for whoever views it.
 * Without a token or without access it is the key and the hint, as a chip.
 */
export function IssueBlock({ issueKey, links = true }: { issueKey: string; links?: boolean }) {
  const state = useChip(issueKey);
  const frame = {
    role: "group",
    "aria-label": t.armature.block.label(issueKey),
    "data-armature-issue-block": issueKey,
    "data-state": state.kind,
  } as const;
  if (state.kind !== "issue") {
    return (
      <div {...frame} className="doc-block">
        <IssueChip issueKey={issueKey} links={links} />
      </div>
    );
  }
  return (
    <div {...frame} className="doc-block space-y-2">
      <IssueCard issue={state.issue} links={links} />
    </div>
  );
}

function IssueCard({ issue, links }: { issue: ArmatureIssue; links: boolean }) {
  const c = t.armature.chip;
  const b = t.armature.block;
  const field = (label: string, value: ReactNode, name: string) => (
    <div className="flex min-w-0 items-baseline gap-2" data-field={name}>
      <dt className="w-20 shrink-0 text-ink-muted">{label}</dt>
      <dd className="m-0 inline-flex min-w-0 items-center gap-1">{value}</dd>
    </div>
  );
  return (
    <>
      <div className="flex flex-wrap items-center gap-2">
        <TypeIcon icon={issue.type.icon} name={issue.type.name} />
        {/* An issue that moved shows the key it has now; the page keeps the one it was named by. */}
        <span className="font-medium" data-issue-key>
          {issue.key}
        </span>
        <StatusLozenge name={issue.status.name} category={issue.status.category} />
      </div>
      <div className="font-medium text-ink" data-issue-summary>
        {issue.summary}
      </div>
      <dl className="m-0 grid gap-x-6 gap-y-1 text-sm sm:grid-cols-2">
        {field(
          c.type,
          <>
            <TypeIcon icon={issue.type.icon} name="" />
            {issue.type.name}
          </>,
          "type",
        )}
        {field(c.priority, c.priorities[issue.priority] ?? issue.priority, "priority")}
        {field(c.assignee, issue.assignee?.name ?? c.unassigned, "assignee")}
        {field(b.reporter, issue.reporter?.name ?? b.none, "reporter")}
        {field(b.due, formatDue(issue.dueDate) ?? b.none, "due")}
        {field(c.updated, changedAt.format(new Date(issue.updatedAt)), "updated")}
      </dl>
      {links && (
        <a href={issue.url} target="_blank" rel="noopener noreferrer" className="inline-flex items-center gap-1 text-sm" data-action="open-in-armature">
          {b.open}
          <span className="sr-only">{b.openRest(issue.key)}</span>
          <Icon.External />
        </a>
      )}
    </>
  );
}
