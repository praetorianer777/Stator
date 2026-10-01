import { useId, useState, type FormEvent } from "react";
import { ApiError } from "@/api/client";
import { useArmatureIssueTypes, useArmatureProjects, useCreateArmatureIssues, type ArmatureCreated } from "@/api/armature";
import { Button, Dialog, ErrorBanner, IconButton, Input, Labelled, Select } from "@/components/ui";
import { Icon } from "@/components/icons";
import { ARMATURE_SECTION_ID, ARMATURE_SUMMARY_MAX_LENGTH, PROFILE_PATH } from "@/config";
import { t } from "@/i18n";
import type { SelectionPlan } from "@/features/editor/issueSelection";

/** What became of each item a create sent, in the order it was sent. */
export type ItemOutcome = { at: number; summary: string } & (
  | { kind: "created"; key: string }
  | { kind: "failed"; message: string; unreachable: boolean }
  | { kind: "skipped" }
);

/**
 * The keys of a create's answer laid over the plan's items: sent holds the
 * plan index of each item sent, in order; an item left out or not made is null.
 */
export function keysFor(planSize: number, sent: readonly number[], answer: ArmatureCreated): (string | null)[] {
  const keys: (string | null)[] = Array.from({ length: planSize }, () => null);
  answer.issues.forEach((issue, i) => {
    const at = sent[i];
    if (at !== undefined) keys[at] = issue.key;
  });
  return keys;
}

/** Each sent item's outcome, by its place in the plan: made, refused with Armature's words, or not tried after a refusal. */
export function outcomes(sent: readonly { at: number; summary: string }[], answer: ArmatureCreated): ItemOutcome[] {
  return sent.map(({ at, summary }, i) => {
    const issue = answer.issues[i];
    if (issue) return { at, kind: "created", key: issue.key, summary: issue.summary };
    if (answer.failed && answer.failed.index === i)
      return { at, kind: "failed", summary, message: answer.failed.message, unreachable: answer.failed.code === "armature_unreachable" };
    return { at, kind: "skipped", summary };
  });
}

/** What to tell the author when the whole create was refused: Armature's sentences on its fields say the most. */
export function refusalText(error: unknown): string | null {
  const c = t.armature.create;
  if (!error) return null;
  if (!(error instanceof ApiError)) return c.failed;
  if (error.code === "armature_unreachable") return c.timeout;
  const fields = Object.values(error.fields);
  return fields.length > 0 ? fields.join(" ") : error.message;
}

interface Row {
  /** The item's place in the plan. */
  at: number;
  summary: string;
  kept: boolean;
}

/**
 * Files the items of a selection as Armature issues: asks for the project and
 * the type, shows each summary to change or leave out, then what was made
 * and what Armature refused.
 */
export function CreateIssuesDialog({
  plan,
  pageId,
  onCreated,
  onClose,
}: {
  plan: SelectionPlan;
  pageId: string;
  /** Each plan item's new key, or null; called once, as soon as Armature answers. */
  onCreated: (keys: (string | null)[]) => void;
  onClose: () => void;
}) {
  const c = t.armature.create;
  const id = useId();
  const projects = useArmatureProjects(true);
  const types = useArmatureIssueTypes(true);
  const create = useCreateArmatureIssues();
  const [rows, setRows] = useState<Row[]>(() => plan.items.map((item, at) => ({ at, summary: item.summary, kept: true })));
  const [project, setProject] = useState("");
  const [typeId, setTypeId] = useState("");
  const [problem, setProblem] = useState<string | null>(null);
  const [result, setResult] = useState<ItemOutcome[] | null>(null);

  const creatable = projects.data?.status === "ok" ? projects.data.projects.filter((p) => p.canCreate) : [];
  const chosen = creatable.some((p) => p.key === project) ? project : (creatable[0]?.key ?? "");
  const kept = rows.filter((row) => row.kept);
  const status = projects.data?.status;

  function submit(event: FormEvent) {
    event.preventDefault();
    // The page's own form is this one's ancestor in React's tree.
    event.stopPropagation();
    if (create.isPending || result) return;
    if (kept.length === 0) return setProblem(c.noneLeft);
    if (kept.some((row) => !row.summary.trim())) return setProblem(c.summaryBlank);
    if (!chosen) return setProblem(c.noProjects);
    setProblem(null);
    const sent = kept.map((row) => ({ at: row.at, summary: row.summary.trim() }));
    create.mutate(
      {
        pageId,
        projectKey: chosen,
        ...(typeId ? { typeId } : {}),
        items: sent.map(({ summary }) => ({ summary })),
      },
      {
        onSuccess: (answer) => {
          onCreated(
            keysFor(
              plan.items.length,
              sent.map((row) => row.at),
              answer,
            ),
          );
          setResult(outcomes(sent, answer));
        },
      },
    );
  }

  const failureText = refusalText(create.error);
  const n = kept.length;

  return (
    <Dialog title={c.title(plan.items.length)} onClose={onClose} wide data-create-issues-dialog="">
      {result ? (
        <div className="space-y-3">
          <p role="status" className="text-sm text-ink">
            {c.done(result.filter((r) => r.kind === "created").length, result.length)}
          </p>
          <ul className="space-y-1 text-sm" data-create-results="">
            {result.map((r) => (
              <li key={r.at} data-result={r.kind} className="flex items-start gap-2">
                {r.kind === "created" ? (
                  <>
                    <Icon.Check className="mt-0.5 shrink-0 text-success" aria-hidden="true" />
                    <span>
                      <span className="font-medium" data-created-key={r.key}>
                        {r.key}
                      </span>{" "}
                      {r.summary}
                    </span>
                  </>
                ) : r.kind === "failed" ? (
                  <>
                    <Icon.Warning className="mt-0.5 shrink-0 text-danger" aria-hidden="true" />
                    <span>
                      <span className="font-medium">{r.summary}</span>: {r.message}
                      {r.unreachable && ` ${c.maybeMade}`}
                    </span>
                  </>
                ) : (
                  <>
                    <span aria-hidden="true" className="w-4 shrink-0" />
                    <span className="text-ink-muted">
                      {r.summary}: {c.skipped}
                    </span>
                  </>
                )}
              </li>
            ))}
          </ul>
          <div className="flex justify-end pt-1">
            <Button type="button" onClick={onClose} data-action="close-created">
              {c.close}
            </Button>
          </div>
        </div>
      ) : (
        <form onSubmit={submit} className="space-y-4" noValidate>
          {projects.isPending && (
            <p role="status" className="text-sm text-ink-muted">
              {c.loading}
            </p>
          )}
          {(status === "not_connected" || status === "not_configured") && (
            <p className="text-sm text-ink-muted">
              {c.connect}{" "}
              <a href={`${PROFILE_PATH}#${ARMATURE_SECTION_ID}`} className="underline">
                {c.connectLink}
              </a>
            </p>
          )}
          {status === "rejected" && <ErrorBanner>{c.rejected}</ErrorBanner>}
          {(status === "unreachable" || projects.isError) && <ErrorBanner>{c.unreachable}</ErrorBanner>}
          {status === "ok" && creatable.length === 0 && <ErrorBanner>{c.noProjects}</ErrorBanner>}
          {status === "ok" && creatable.length > 0 && (
            <div className="grid gap-3 sm:grid-cols-2">
              <Select label={c.project} id={`${id}-project`} value={chosen} onChange={(e) => setProject(e.target.value)} data-field="project">
                {creatable.map((p) => (
                  <option key={p.key} value={p.key}>
                    {c.projectOption(p.key, p.name)}
                  </option>
                ))}
              </Select>
              <Select label={c.type} id={`${id}-type`} value={typeId} onChange={(e) => setTypeId(e.target.value)} data-field="type">
                <option value="">{c.defaultType}</option>
                {(types.data?.status === "ok" ? types.data.issueTypes : []).map((type) => (
                  <option key={type.id} value={type.id}>
                    {type.name}
                  </option>
                ))}
              </Select>
            </div>
          )}
          <fieldset className="space-y-2">
            <legend className="text-sm font-medium text-ink-muted">{c.items}</legend>
            <p className="text-sm text-ink-muted">{plan.kind === "text" ? c.replaces : c.follows}</p>
            {plan.dropped > 0 && <p className="text-sm text-ink-muted">{c.dropped(plan.dropped, plan.items.length)}</p>}
            <ol className="space-y-2" data-create-items="">
              {rows.map((row) =>
                row.kept ? (
                  <li key={row.at} className="flex items-end gap-2" data-create-item={row.at}>
                    <Labelled id={`${id}-item-${row.at}`} label={c.summary(row.at + 1)} className="min-w-0 flex-1">
                      <Input
                        id={`${id}-item-${row.at}`}
                        value={row.summary}
                        maxLength={ARMATURE_SUMMARY_MAX_LENGTH}
                        onChange={(e) => {
                          const summary = e.target.value;
                          setRows((now) => now.map((r) => (r.at === row.at ? { ...r, summary } : r)));
                        }}
                      />
                    </Labelled>
                    {kept.length > 1 && (
                      <IconButton
                        icon={<Icon.X />}
                        label={c.leaveOut(row.at + 1)}
                        size="sm"
                        onClick={() => setRows((now) => now.map((r) => (r.at === row.at ? { ...r, kept: false } : r)))}
                      />
                    )}
                  </li>
                ) : null,
              )}
            </ol>
          </fieldset>
          {problem && <ErrorBanner>{problem}</ErrorBanner>}
          {failureText && <ErrorBanner>{failureText}</ErrorBanner>}
          {create.isPending && (
            <p role="status" className="text-sm text-ink-muted">
              {c.creating}
            </p>
          )}
          <div className="flex justify-end gap-2 pt-1">
            <Button type="button" variant="secondary" onClick={onClose}>
              {c.cancel}
            </Button>
            <Button type="submit" disabled={status !== "ok" || creatable.length === 0 || create.isPending || n === 0} data-action="create-issues">
              {c.submit(n)}
            </Button>
          </div>
        </form>
      )}
    </Dialog>
  );
}
