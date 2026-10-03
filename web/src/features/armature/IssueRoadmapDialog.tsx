import { useId, useState, type FormEvent } from "react";
import { useArmatureAccount, useArmatureProjects } from "@/api/armature";
import { Button, Dialog, Labelled, Select, Textarea } from "@/components/ui";
import { describedBy } from "@/components/ui/controls";
import { ARMATURE_QUERY_MAX_LENGTH, ARMATURE_ROADMAP_GROUPINGS } from "@/config";
import { t } from "@/i18n";
import type { RoadmapSettings } from "./roadmap";

type Problems = Partial<Record<"project" | "query", string>>;

/** The settings as the server takes them, or a sentence for each field that is wrong. */
export function checkRoadmap(settings: RoadmapSettings): { settings: RoadmapSettings } | { problems: Problems } {
  const problems: Problems = {};
  if (!/^[A-Z][A-Z0-9]{1,9}$/.test(settings.project)) problems.project = t.armature.roadmapDialog.projectNone;
  if (!settings.query.trim()) problems.query = t.armature.listDialog.queryBlank;
  else if ([...settings.query].length > ARMATURE_QUERY_MAX_LENGTH) problems.query = t.armature.listDialog.queryLong(ARMATURE_QUERY_MAX_LENGTH);
  return Object.keys(problems).length > 0 ? { problems } : { settings };
}

/** Asks for a roadmap's project, the query its issues must match, and what to group them by. */
export function IssueRoadmapDialog({
  initial,
  isNew,
  onSave,
  onClose,
}: {
  initial: RoadmapSettings;
  isNew: boolean;
  onSave: (settings: RoadmapSettings) => void;
  onClose: () => void;
}) {
  const d = t.armature.roadmapDialog;
  const id = useId();
  const account = useArmatureAccount();
  const connected = Boolean(account.data?.connected && account.data.status !== "rejected");
  const projects = useArmatureProjects(connected);
  const known = projects.data?.status === "ok" ? projects.data.projects : [];
  const [settings, setSettings] = useState<RoadmapSettings>(initial);
  const [problems, setProblems] = useState<Problems>({});
  const set = (change: Partial<RoadmapSettings>) => setSettings((was) => ({ ...was, ...change }));
  const project = settings.project || known[0]?.key || "";
  // A new roadmap's query follows the project chosen until the author writes their own.
  const query = isNew && !settings.query && project ? `project = ${project} AND statusCategory != done` : settings.query;

  function submit(event: FormEvent) {
    event.preventDefault();
    // The page's own form is this one's ancestor in React's tree.
    event.stopPropagation();
    const checked = checkRoadmap({ ...settings, project, query });
    if ("problems" in checked) {
      setProblems(checked.problems);
      return;
    }
    onSave(checked.settings);
  }

  const queryId = `${id}-query`;
  return (
    <Dialog title={isNew ? d.titleNew : d.titleEdit} onClose={onClose} wide data-issue-roadmap-dialog="">
      <form onSubmit={submit} className="space-y-4" noValidate>
        <Select
          label={d.project}
          value={project}
          error={problems.project}
          hint={!connected ? t.armature.chartDialog.cannotList : undefined}
          onChange={(event) => {
            set({ project: event.target.value });
            setProblems(({ project: _, ...rest }) => rest);
          }}
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
            value={query}
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
          <legend className="text-sm font-medium text-ink-muted">{d.groupBy}</legend>
          {ARMATURE_ROADMAP_GROUPINGS.map((g) => (
            <label key={g} className="flex items-center gap-2 text-sm text-ink">
              <input
                type="radio"
                name={`${id}-group`}
                checked={settings.groupBy === g}
                onChange={() => set({ groupBy: g })}
                className="accent-accent"
                data-roadmap-group-by={g}
              />
              {d.groupings[g]}
            </label>
          ))}
        </fieldset>
        <div className="flex justify-end gap-2 pt-1">
          <Button type="button" variant="secondary" onClick={onClose}>
            {t.armature.listDialog.cancel}
          </Button>
          <Button type="submit" data-action="save-issue-roadmap">
            {isNew ? d.insert : d.save}
          </Button>
        </div>
      </form>
    </Dialog>
  );
}
