import { subjectKey, type Subject } from "@/api/permissions";
import { Icon } from "@/components/icons";
import { t } from "@/i18n";
import { SubjectGlyph } from "./SubjectPicker";

/** The people and groups a grant or a list names, each with a way to take it off when onRemove is given. */
export function SubjectList({
  subjects,
  onRemove,
  empty,
  selfId,
}: {
  subjects: Subject[];
  onRemove?: (subject: Subject) => void;
  empty: string;
  selfId?: string;
}) {
  if (subjects.length === 0) return <p className="text-sm text-ink-subtle">{empty}</p>;
  return (
    <ul className="flex flex-wrap gap-1.5" data-subject-list>
      {subjects.map((subject) => (
        <li
          key={subjectKey(subject)}
          className="inline-flex h-7 items-center gap-1.5 rounded-control border border-border bg-surface-raised pr-1 pl-2 text-sm text-ink"
          data-subject={subject.name}
          data-subject-type={subject.type}
        >
          <SubjectGlyph type={subject.type} />
          <span>
            {subject.name}
            {subject.type === "user" && subject.id === selfId && <span className="text-ink-subtle"> ({t.permissions.you})</span>}
          </span>
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
      ))}
    </ul>
  );
}
