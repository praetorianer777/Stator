import { useId, useState, type FormEvent } from "react";
import { useArmatureAccount, useArmatureProjects } from "@/api/armature";
import { Button, Dialog, Labelled, Select, Textarea } from "@/components/ui";
import { describedBy } from "@/components/ui/controls";
import { ARMATURE_CHART_DAY_CHOICES, ARMATURE_CHART_GROUPINGS, ARMATURE_QUERY_MAX_LENGTH, type ArmatureChartGrouping } from "@/config";
import { t } from "@/i18n";
import type { ArmatureChartKind, ChartSettings } from "./chart";

type Problems = Partial<Record<"project" | "query", string>>;

/** The settings as the server takes them, or a sentence for each field that is wrong. */
export function checkChart(settings: ChartSettings): { settings: ChartSettings } | { problems: Problems } {
  const d = t.armature.chartDialog;
  const problems: Problems = {};
  if (!/^[A-Z][A-Z0-9]{1,9}$/.test(settings.project)) problems.project = d.projectNone;
  if (!settings.query.trim()) problems.query = t.armature.listDialog.queryBlank;
  else if ([...settings.query].length > ARMATURE_QUERY_MAX_LENGTH) problems.query = t.armature.listDialog.queryLong(ARMATURE_QUERY_MAX_LENGTH);
  return Object.keys(problems).length > 0 ? { problems } : { settings };
}

/** Asks for a chart's project, the query its issues must match, and how to draw them. */
export function IssueChartDialog({
  initial,
  isNew,
  onSave,
  onClose,
}: {
  initial: ChartSettings;
  isNew: boolean;
  onSave: (settings: ChartSettings) => void;
  onClose: () => void;
}) {
  const d = t.armature.chartDialog;
  const c = t.armature.chart;
  const id = useId();
  const account = useArmatureAccount();
  const connected = Boolean(account.data?.connected && account.data.status !== "rejected");
  const projects = useArmatureProjects(connected);
  const known = projects.data?.status === "ok" ? projects.data.projects : [];
  const [settings, setSettings] = useState<ChartSettings>(initial);
  const [problems, setProblems] = useState<Problems>({});
  const set = (change: Partial<ChartSettings>) => setSettings((was) => ({ ...was, ...change }));
  const project = settings.project || known[0]?.key || "";

  function submit(event: FormEvent) {
    event.preventDefault();
    // The page's own form is this one's ancestor in React's tree.
    event.stopPropagation();
    const checked = checkChart({ ...settings, project });
    if ("problems" in checked) {
      setProblems(checked.problems);
      return;
    }
    onSave(checked.settings);
  }

  const queryId = `${id}-query`;
  return (
    <Dialog title={isNew ? d.titleNew : d.titleEdit} onClose={onClose} wide data-issue-chart-dialog="">
      <form onSubmit={submit} className="space-y-4" noValidate>
        <Select
          label={d.project}
          value={project}
          error={problems.project}
          hint={!connected ? d.cannotList : undefined}
          onChange={(event) => {
            set({ project: event.target.value });
            setProblems(({ project: _, ...rest }) => rest);
          }}
          data-chart-project=""
        >
          {known.length === 0 && settings.project && <option value={settings.project}>{settings.project}</option>}
          {known.map((p) => (
            <option key={p.key} value={p.key}>
              {p.name} ({p.key})
            </option>
          ))}
        </Select>
        <Labelled id={queryId} label={d.query} hint={d.queryHint} error={problems.query}>
          <Textarea
            id={queryId}
            rows={3}
            value={settings.query}
            spellCheck={false}
            invalid={Boolean(problems.query)}
            aria-describedby={describedBy(queryId, d.queryHint, problems.query)}
            onChange={(event) => {
              set({ query: event.target.value });
              setProblems(({ query: _, ...rest }) => rest);
            }}
          />
        </Labelled>
        <fieldset className="space-y-1">
          <legend className="text-sm font-medium text-ink-muted">{d.chart}</legend>
          {(["pie", "createdResolved"] as ArmatureChartKind[]).map((kind) => (
            <label key={kind} className="flex items-center gap-2 text-sm text-ink">
              <input
                type="radio"
                name={`${id}-chart`}
                checked={settings.chart === kind}
                onChange={() => set({ chart: kind })}
                className="accent-accent"
                data-chart-kind={kind}
              />
              {d.kinds[kind]}
            </label>
          ))}
        </fieldset>
        {settings.chart === "pie" ? (
          <Select
            label={d.groupBy}
            value={settings.groupBy}
            onChange={(event) => set({ groupBy: event.target.value as ArmatureChartGrouping })}
            data-chart-group-by=""
          >
            {ARMATURE_CHART_GROUPINGS.map((g) => (
              <option key={g} value={g}>
                {c.groupings[g]}
              </option>
            ))}
          </Select>
        ) : (
          <Select label={d.days} value={String(settings.days)} onChange={(event) => set({ days: Number(event.target.value) })} data-chart-days="">
            {ARMATURE_CHART_DAY_CHOICES.map((n) => (
              <option key={n} value={n}>
                {d.dayChoice(n)}
              </option>
            ))}
          </Select>
        )}
        <div className="flex justify-end gap-2 pt-1">
          <Button type="button" variant="secondary" onClick={onClose}>
            {t.armature.listDialog.cancel}
          </Button>
          <Button type="submit" data-action="save-issue-chart">
            {isNew ? d.insert : d.save}
          </Button>
        </div>
      </form>
    </Dialog>
  );
}
