import { useEffect, useId, useState, type KeyboardEvent } from "react";
import { subjectKey, useSubjectSearch, type Subject } from "@/api/permissions";
import { Icon } from "@/components/icons";
import { ErrorBanner, Input, cx } from "@/components/ui";
import { PICKER_DEBOUNCE_MS } from "@/config";
import { t } from "@/i18n";

interface Option {
  subject: Subject;
  detail: string;
}

function useDebounced<T>(value: T, ms: number): T {
  const [settled, setSettled] = useState(value);
  useEffect(() => {
    const timer = window.setTimeout(() => setSettled(value), ms);
    return () => window.clearTimeout(timer);
  }, [value, ms]);
  return settled;
}

/**
 * A box that finds people and groups by the start of a name or an email and
 * hands the chosen one back; a combobox, so the arrows walk the options.
 */
export function SubjectPicker({
  label,
  onPick,
  exclude = [],
  allowEveryone = false,
  disabled,
}: {
  label: string;
  onPick: (subject: Subject) => void;
  /** Subjects already chosen, by subjectKey, which are not offered again. */
  exclude?: string[];
  allowEveryone?: boolean;
  disabled?: boolean;
}) {
  const id = useId();
  const listId = `${id}-options`;
  const [text, setText] = useState("");
  const [open, setOpen] = useState(false);
  const [active, setActive] = useState(0);
  const q = useDebounced(text.trim(), PICKER_DEBOUNCE_MS);
  const found = useSubjectSearch(q, open);

  const options: Option[] = [];
  if (allowEveryone && t.permissions.everyone.toLowerCase().startsWith(q.toLowerCase())) {
    options.push({ subject: { type: "everyone", id: null, name: t.permissions.everyone }, detail: t.permissions.everyoneDetail });
  }
  for (const group of found.groups)
    options.push({ subject: { type: "group", id: group.id, name: group.name }, detail: t.permissions.members(group.memberCount) });
  for (const person of found.people) options.push({ subject: { type: "user", id: person.id, name: person.name || person.email }, detail: person.email });
  const shown = options.filter((option) => !exclude.includes(subjectKey(option.subject)));
  const current = Math.min(active, Math.max(shown.length - 1, 0));

  function pick(option: Option) {
    onPick(option.subject);
    setOpen(false);
    setText("");
    setActive(0);
  }

  function onKeyDown(event: KeyboardEvent<HTMLInputElement>) {
    if (event.key === "ArrowDown") {
      event.preventDefault();
      if (!open) setOpen(true);
      else setActive(Math.min(current + 1, shown.length - 1));
    } else if (event.key === "ArrowUp") {
      event.preventDefault();
      setActive(Math.max(current - 1, 0));
    } else if (event.key === "Enter") {
      // Enter picks rather than submitting the form the picker sits in.
      event.preventDefault();
      const option = shown[current];
      if (open && option) pick(option);
    } else if (event.key === "Escape" && open) {
      // Closes the list, not the dialog around it.
      event.stopPropagation();
      setOpen(false);
    }
  }

  const expanded = open && !disabled;
  return (
    <div className="space-y-1" data-subject-picker>
      <label htmlFor={id} className="block text-sm font-medium text-ink-muted">
        {label}
      </label>
      {/* The list floats over what follows, so opening and closing it never moves a button under the pointer. */}
      <div className="relative">
        <Input
          id={id}
          role="combobox"
          aria-expanded={expanded && shown.length > 0}
          aria-controls={listId}
          aria-autocomplete="list"
          aria-activedescendant={expanded && shown[current] ? `${listId}-${current}` : undefined}
          autoComplete="off"
          placeholder={t.permissions.pickerPlaceholder}
          value={text}
          disabled={disabled}
          onChange={(event) => {
            setText(event.target.value);
            setOpen(true);
            setActive(0);
          }}
          onFocus={() => setOpen(true)}
          onBlur={() => setOpen(false)}
          onKeyDown={onKeyDown}
        />
        <div
          id={listId}
          role="listbox"
          aria-label={t.permissions.pickerOptions}
          hidden={!expanded || shown.length === 0}
          className="absolute inset-x-0 top-full z-30 mt-1 max-h-56 overflow-y-auto rounded-overlay border border-border bg-surface-overlay p-1 shadow-2"
          data-subject-options
        >
          {shown.map((option, i) => (
            <div
              key={subjectKey(option.subject)}
              id={`${listId}-${i}`}
              role="option"
              aria-selected={i === current}
              tabIndex={-1}
              // The press would take focus from the box and close the list before the click lands.
              onMouseDown={(event) => {
                event.preventDefault();
                pick(option);
              }}
              onMouseEnter={() => setActive(i)}
              className={cx(
                "flex cursor-pointer items-center gap-2 rounded-control px-2 py-1.5 text-sm",
                i === current ? "bg-surface-raised text-ink" : "text-ink-muted",
              )}
              data-subject-option={option.subject.name}
            >
              <SubjectGlyph type={option.subject.type} />
              <span className="font-medium text-ink">{option.subject.name}</span>
              <span className="min-w-0 truncate text-xs text-ink-subtle">{option.detail}</span>
            </div>
          ))}
        </div>
        <p
          role="status"
          className="absolute inset-x-0 top-full z-30 mt-1 rounded-overlay border border-border bg-surface-overlay px-2 py-1.5 text-sm text-ink-subtle shadow-2 empty:hidden"
        >
          {expanded && shown.length === 0 ? (found.isFetching ? t.permissions.pickerLoading : t.permissions.pickerEmpty) : ""}
        </p>
      </div>
      {found.error && <ErrorBanner>{found.error.message}</ErrorBanner>}
    </div>
  );
}

/** A person, a group or everyone at a glance; the name beside it says which in words. */
export function SubjectGlyph({ type }: { type: Subject["type"] }) {
  const Glyph = type === "user" ? Icon.User : type === "group" ? Icon.Users : Icon.Space;
  return <Glyph className="shrink-0 text-ink-subtle" />;
}
