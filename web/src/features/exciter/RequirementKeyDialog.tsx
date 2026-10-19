import { useId, useState } from "react";
import { Button, Dialog, Input, Labelled } from "@/components/ui";
import { describedBy } from "@/components/ui/controls";
import { EXCITER_KEY_MAX_LENGTH } from "@/config";
import { t } from "@/i18n";
import { normalizeRequirementKey } from "./requirementKeys";

/** Asks for the key of the requirement a chip names. */
export function RequirementKeyDialog({ onInsert, onClose }: { onInsert: (key: string) => void; onClose: () => void }) {
  const p = t.exciter.picker;
  const id = useId();
  const [text, setText] = useState("");
  const [tried, setTried] = useState(false);
  const key = normalizeRequirementKey(text);
  const problem = tried && !key ? p.invalid : undefined;
  const fieldId = `${id}-key`;
  return (
    <Dialog title={p.title} onClose={onClose} data-exciter-key-dialog="">
      <form
        noValidate
        className="space-y-3"
        onSubmit={(event) => {
          // The page's own form is this one's ancestor in React's tree.
          event.preventDefault();
          event.stopPropagation();
          setTried(true);
          if (key) onInsert(key);
        }}
      >
        <Labelled id={fieldId} label={p.field} hint={p.hint} error={problem}>
          <Input
            id={fieldId}
            value={text}
            autoComplete="off"
            maxLength={EXCITER_KEY_MAX_LENGTH}
            invalid={Boolean(problem)}
            aria-describedby={describedBy(fieldId, p.hint, problem)}
            onChange={(event) => {
              setText(event.target.value);
              setTried(false);
            }}
          />
        </Labelled>
        <div className="flex justify-end gap-2 pt-1">
          <Button variant="ghost" onClick={onClose}>
            {p.cancel}
          </Button>
          <Button type="submit" data-action="insert-exciter-requirement">
            {p.insert}
          </Button>
        </div>
      </form>
    </Dialog>
  );
}
