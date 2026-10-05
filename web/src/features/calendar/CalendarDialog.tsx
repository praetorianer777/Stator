import { useState, type FormEvent } from "react";
import { ApiError } from "@/api/client";
import { useArmatureAccount, useArmatureProjects } from "@/api/armature";
import { useCalendars, useCreateCalendar } from "@/api/calendars";
import { useSpaces } from "@/api/spaces";
import { Button, Dialog, ErrorBanner, Field, Select } from "@/components/ui";
import { CALENDAR_NAME_MAX_LENGTH } from "@/config";
import { t } from "@/i18n";
import type { CalendarSettings } from "./calendar";

// The calendar choice that names a new calendar instead of one there is.
const NEW = "new";

type Problems = Partial<Record<"space" | "name", string>>;

/**
 * Asks which calendar a block draws, a space's own or a new one named here,
 * and which Armature project's due issues go beside its events.
 */
export function CalendarDialog({
  initial,
  spaceKey,
  isNew,
  onSave,
  onClose,
}: {
  initial: CalendarSettings;
  /** The space the page is in, which a new block starts in. */
  spaceKey: string | null;
  isNew: boolean;
  onSave: (settings: CalendarSettings) => void;
  onClose: () => void;
}) {
  const d = t.calendar.dialog;
  const spaces = useSpaces();
  const [space, setSpace] = useState(spaceKey ?? "");
  const calendars = useCalendars(space || null);
  const listed = calendars.data ?? [];
  const [picked, setPicked] = useState(initial.calendarId);
  const [name, setName] = useState("");
  const [project, setProject] = useState(initial.project ?? "");
  const [problems, setProblems] = useState<Problems>({});
  const [failed, setFailed] = useState<string | null>(null);
  const create = useCreateCalendar();
  const account = useArmatureAccount();
  const configured = Boolean(account.data?.configured && account.data.baseUrl);
  const connected = Boolean(configured && account.data?.connected && account.data.status !== "rejected");
  const projects = useArmatureProjects(connected);
  const known = projects.data?.status === "ok" ? projects.data.projects : [];
  // Until the author picks, the block's calendar or the space's first; a space without one names a new one.
  const chosen = picked === NEW || listed.some((c) => c.id === picked) || (picked !== "" && picked === initial.calendarId);
  const choice = chosen ? picked : (listed[0]?.id ?? NEW);

  async function submit(event: FormEvent) {
    event.preventDefault();
    // The page's own form is this one's ancestor in React's tree.
    event.stopPropagation();
    const chosenProject = project || null;
    if (choice !== NEW) {
      onSave({ calendarId: choice, project: chosenProject });
      return;
    }
    if (!space) {
      setProblems({ space: d.pickSpace });
      return;
    }
    const trimmed = name.trim();
    if (!trimmed) {
      setProblems({ name: d.nameBlank });
      return;
    }
    if ([...trimmed].length > CALENDAR_NAME_MAX_LENGTH) {
      setProblems({ name: d.nameLong(CALENDAR_NAME_MAX_LENGTH) });
      return;
    }
    try {
      const made = await create.mutateAsync({ spaceKey: space, name: trimmed });
      onSave({ calendarId: made.id, project: chosenProject });
    } catch (error) {
      if (error instanceof ApiError && error.fields.name) setProblems({ name: error.fields.name });
      else setFailed(error instanceof ApiError ? error.message : d.failed);
    }
  }

  return (
    <Dialog title={isNew ? d.titleNew : d.titleEdit} onClose={onClose} data-calendar-dialog="">
      <form onSubmit={submit} className="space-y-4" noValidate>
        <Select
          label={d.space}
          value={space}
          error={problems.space}
          onChange={(event) => {
            setSpace(event.target.value);
            setPicked("");
            setProblems({});
          }}
        >
          {!space && <option value="">{d.pickSpace}</option>}
          {space && !spaces.data?.some((s) => s.key === space) && <option value={space}>{space}</option>}
          {(spaces.data ?? []).map((s) => (
            <option key={s.key} value={s.key}>
              {s.name} ({s.key})
            </option>
          ))}
        </Select>
        <Select
          label={d.calendar}
          value={choice}
          hint={space && calendars.isSuccess && listed.length === 0 ? d.noCalendars : undefined}
          onChange={(event) => {
            setPicked(event.target.value);
            setProblems({});
          }}
        >
          {initial.calendarId && !listed.some((c) => c.id === initial.calendarId) && <option value={initial.calendarId}>{t.calendar.untitled}</option>}
          {listed.map((c) => (
            <option key={c.id} value={c.id}>
              {c.name}
            </option>
          ))}
          <option value={NEW}>{d.newCalendar}</option>
        </Select>
        {choice === NEW && (
          <Field
            label={d.name}
            value={name}
            error={problems.name}
            maxLength={CALENDAR_NAME_MAX_LENGTH}
            onChange={(event) => {
              setName(event.target.value);
              setProblems(({ name: _, ...rest }) => rest);
            }}
          />
        )}
        {configured && (
          <Select
            label={d.project}
            value={project}
            hint={connected ? d.projectHint : d.cannotList}
            onChange={(event) => setProject(event.target.value)}
            data-calendar-project=""
          >
            <option value="">{d.noProject}</option>
            {project && !known.some((p) => p.key === project) && <option value={project}>{project}</option>}
            {known.map((p) => (
              <option key={p.key} value={p.key}>
                {p.name} ({p.key})
              </option>
            ))}
          </Select>
        )}
        {failed && <ErrorBanner>{failed}</ErrorBanner>}
        <div className="flex justify-end gap-2 pt-1">
          <Button type="button" variant="secondary" onClick={onClose}>
            {d.cancel}
          </Button>
          <Button type="submit" disabled={create.isPending} data-action="save-calendar">
            {isNew ? d.insert : d.save}
          </Button>
        </div>
      </form>
    </Dialog>
  );
}
