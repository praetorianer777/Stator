import { useId, useState, type FormEvent, type ReactNode } from "react";
import { Button, Dialog, Input, Labelled } from "@/components/ui";
import { describedBy } from "@/components/ui/controls";
import { STATUS_LABEL_MAX_LENGTH } from "@/config";
import { t } from "@/i18n";
import { STATUS_COLORS, type StatusColor } from "./schema";
import { StatusLabel, isoDay, statusLabel, type StatusAttrs } from "./InlineValueViews";

function Actions() {
  return (
    <div className="flex justify-end gap-2 pt-1">
      <Button type="submit" data-action="save-inline-value">
        {t.inlineValues.save}
      </Button>
    </div>
  );
}

// The page's own form is this one's ancestor in React's tree, so a submit
// here must not reach it.
function submitted(event: FormEvent, then: () => void) {
  event.preventDefault();
  event.stopPropagation();
  then();
}

function Form({ onSubmit, children }: { onSubmit: () => void; children: ReactNode }) {
  return (
    <form onSubmit={(event) => submitted(event, onSubmit)} className="space-y-3" noValidate>
      {children}
      <Actions />
    </form>
  );
}

/** Asks for a status's words and colour, showing the label as it will look. */
export function StatusDialog({ initial, onSave, onClose }: { initial: StatusAttrs; onSave: (attrs: StatusAttrs) => void; onClose: () => void }) {
  const s = t.inlineValues.status;
  const id = useId();
  const [text, setText] = useState(initial.label);
  const [color, setColor] = useState<StatusColor>(initial.color);
  const [tried, setTried] = useState(false);
  const label = statusLabel(text.trim());
  const problem = tried && !label ? s.empty : undefined;
  const fieldId = `${id}-label`;
  const hint = s.hint(STATUS_LABEL_MAX_LENGTH);
  return (
    <Dialog title={s.dialog} onClose={onClose} data-status-dialog="">
      <Form
        onSubmit={() => {
          setTried(true);
          if (label) onSave({ label, color });
        }}
      >
        <Labelled id={fieldId} label={s.field} hint={hint} error={problem}>
          <Input
            id={fieldId}
            value={text}
            maxLength={STATUS_LABEL_MAX_LENGTH}
            autoComplete="off"
            invalid={Boolean(problem)}
            aria-describedby={describedBy(fieldId, hint, problem)}
            onChange={(event) => {
              setText(event.target.value);
              setTried(false);
            }}
          />
        </Labelled>
        <fieldset className="space-y-1">
          <legend className="text-sm font-medium text-ink-muted">{s.colour}</legend>
          <div className="flex flex-wrap gap-x-4 gap-y-2">
            {STATUS_COLORS.map((option) => (
              <label key={option} className="inline-flex items-center gap-1.5 text-sm text-ink">
                <input
                  type="radio"
                  name={`${id}-colour`}
                  value={option}
                  checked={color === option}
                  onChange={() => setColor(option)}
                  className="accent-accent"
                  data-status-colour={option}
                />
                <span aria-hidden="true" className="doc-status-swatch" data-status-label={option} />
                {s.colours[option]}
              </label>
            ))}
          </div>
        </fieldset>
        <p className="flex items-center gap-2 text-sm text-ink-muted" data-status-preview="">
          {s.preview}
          <StatusLabel label={label ?? initial.label} color={color} />
        </p>
      </Form>
    </Dialog>
  );
}

/** Asks for a day with the browser's own date field, which reads and writes the reader's format. */
export function DateDialog({ initial, onSave, onClose }: { initial: string; onSave: (date: string) => void; onClose: () => void }) {
  const d = t.inlineValues.date;
  const id = useId();
  const [day, setDay] = useState(isoDay(initial) ?? "");
  const [tried, setTried] = useState(false);
  const picked = isoDay(day);
  const problem = tried && !picked ? d.empty : undefined;
  const fieldId = `${id}-day`;
  return (
    <Dialog title={d.dialog} onClose={onClose} data-date-dialog="">
      <Form
        onSubmit={() => {
          setTried(true);
          if (picked) onSave(picked);
        }}
      >
        <Labelled id={fieldId} label={d.field} error={problem}>
          <Input
            id={fieldId}
            type="date"
            value={day}
            required
            invalid={Boolean(problem)}
            aria-describedby={describedBy(fieldId, undefined, problem)}
            onChange={(event) => {
              setDay(event.target.value);
              setTried(false);
            }}
          />
        </Labelled>
      </Form>
    </Dialog>
  );
}
