import { subjectKey, type Subject } from "@/api/permissions";
import { Icon } from "@/components/icons";
import { cx } from "@/components/ui";
import { t } from "@/i18n";
import { SubjectGlyph } from "./SubjectPicker";

/** The people and groups a grant or a list names, each removable when onRemove is given, with any warning keyed by subjectKey beside it. */
export function SubjectList({
  subjects,
  onRemove,
  empty,
  selfId,
  warnings,
}: {
  subjects: Subject[];
  onRemove?: (subject: Subject) => void;
  empty: string;
  selfId?: string;
  warnings?: Record<string, string>;
}) {
  if (subjects.length === 0) return <p className="text-sm text-ink-subtle">{empty}</p>;
  return (
    <ul className="flex flex-wrap gap-1.5" data-subject-list>
      {subjects.map((subject) => {
        const warning = warnings?.[subjectKey(subject)];
        return (
          <li
            key={subjectKey(subject)}
            className={cx(
              "inline-flex min-h-7 flex-wrap items-center gap-x-1.5 rounded-control border bg-surface-raised pr-1 pl-2 text-sm text-ink",
              warning ? "border-danger" : "border-border",
            )}
            data-subject={subject.name}
            data-subject-type={subject.type}
            data-subject-warning={warning}
          >
            <SubjectGlyph type={subject.type} />
            <span>
              {subject.name}
              {subject.type === "user" && subject.id === selfId && <span className="text-ink-subtle"> ({t.permissions.you})</span>}
            </span>
            {warning && (
              <span className="inline-flex items-center gap-1 text-xs text-danger">
                <Icon.Warning />
                {warning}
              </span>
            )}
            <span className="sr-only">
              , {subject.type === "group" ? t.permissions.group : subject.type === "user" ? t.permissions.person : t.permissions.everyoneDetail}
            </span>
            {onRemove && (
              <button
                type="button"
                onClick={() => onRemove(subject)}
                aria-label={t.permissions.remove(subject.name)}
                className="inline-flex size-5 items-center justify-center rounded-control text-ink-subtle hover:bg-surface hover:text-ink"
                data-action="remove-subject"
              >
                <Icon.X />
              </button>
            )}
            {!onRemove && <span className="w-1" />}
          </li>
        );
      })}
    </ul>
  );
}
