import { useId, useState } from "react";
import { Button, Dialog, Input, Labelled } from "@/components/ui";
import { describedBy } from "@/components/ui/controls";
import { t } from "@/i18n";
import { webAddress } from "./LinkCardViews";

/** Asks for the address a link preview shows. */
export function LinkCardDialog({ onInsert, onClose }: { onInsert: (url: string) => void; onClose: () => void }) {
  const l = t.editor.linkCard;
  const id = useId();
  const [text, setText] = useState("");
  const [tried, setTried] = useState(false);
  const url = webAddress(text);
  const problem = tried && !url ? l.invalid : undefined;
  const fieldId = `${id}-url`;
  return (
    <Dialog title={l.dialog} onClose={onClose} data-link-card-dialog="">
      <form
        noValidate
        className="space-y-3"
        onSubmit={(event) => {
          // The page's own form is this one's ancestor in React's tree.
          event.preventDefault();
          event.stopPropagation();
          setTried(true);
          if (url) onInsert(url);
        }}
      >
        <Labelled id={fieldId} label={l.field} hint={l.hint} error={problem}>
          <Input
            id={fieldId}
            type="url"
            inputMode="url"
            value={text}
            autoComplete="off"
            invalid={Boolean(problem)}
            aria-describedby={describedBy(fieldId, l.hint, problem)}
            onChange={(event) => {
              setText(event.target.value);
              setTried(false);
            }}
          />
        </Labelled>
        <div className="flex justify-end gap-2 pt-1">
          <Button type="submit" data-action="insert-link-card">
            {l.insert}
          </Button>
        </div>
      </form>
    </Dialog>
  );
}
