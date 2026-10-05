import { useState, type FormEvent } from "react";
import { Button, Dialog, Select } from "@/components/ui";
import { CONTRIBUTORS_LIMIT_CHOICES, CONTRIBUTOR_SCOPES } from "@/config";
import { t } from "@/i18n";
import type { ContributorScope, ContributorsSettings } from "./contributors";

/** Asks what a contributors block counts: this page or its tree, and how many people. */
export function ContributorsDialog({
  initial,
  onSave,
  onClose,
}: {
  initial: ContributorsSettings;
  onSave: (settings: ContributorsSettings) => void;
  onClose: () => void;
}) {
  const d = t.contributors.dialog;
  const [scope, setScope] = useState<ContributorScope>(initial.scope);
  const [limit, setLimit] = useState(initial.limit);

  function submit(event: FormEvent) {
    event.preventDefault();
    // The page's own form is this one's ancestor in React's tree.
    event.stopPropagation();
    onSave({ scope, limit });
  }

  return (
    <Dialog title={d.title} onClose={onClose} data-contributors-dialog="">
      <form onSubmit={submit} className="space-y-4" noValidate>
        <Select label={d.scope} value={scope} onChange={(event) => setScope(event.target.value as ContributorScope)}>
          {CONTRIBUTOR_SCOPES.map((choice) => (
            <option key={choice} value={choice}>
              {d.scopes[choice]}
            </option>
          ))}
        </Select>
        <Select label={d.limit} value={String(limit)} onChange={(event) => setLimit(Number(event.target.value))}>
          {[...new Set([...CONTRIBUTORS_LIMIT_CHOICES, initial.limit])]
            .sort((a, b) => a - b)
            .map((n) => (
              <option key={n} value={n}>
                {d.limitChoice(n)}
              </option>
            ))}
        </Select>
        <div className="flex justify-end gap-2 pt-1">
          <Button type="button" variant="secondary" onClick={onClose}>
            {d.cancel}
          </Button>
          <Button type="submit" data-action="save-contributors">
            {d.save}
          </Button>
        </div>
      </form>
    </Dialog>
  );
}
