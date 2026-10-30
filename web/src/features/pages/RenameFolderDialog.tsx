import { useState, type FormEvent } from "react";
import type { Page } from "@/api/pages";
import { useRenameFolder } from "@/api/tree";
import { Button, Dialog, ErrorBanner, Field } from "@/components/ui";
import { PAGE_TITLE_MAX_LENGTH } from "@/config";
import { t } from "@/i18n";

/** Gives a folder a new name; a folder has no editor to do it in. */
export function RenameFolderDialog({ folder, onClose }: { folder: Page; onClose: () => void }) {
  const rename = useRenameFolder(folder.id);
  const [title, setTitle] = useState(folder.title);
  const [error, setError] = useState("");

  function submit(event: FormEvent) {
    event.preventDefault();
    if (!title.trim()) {
      setError(t.page.emptyTitle);
      return;
    }
    setError("");
    rename.mutate({ title, version: folder.version }, { onSuccess: onClose });
  }

  return (
    <Dialog
      open
      title={t.page.renameTitle(folder.title)}
      onClose={onClose}
      attrs={{ "data-rename-dialog": "" }}
      footer={
        <>
          <Button type="button" variant="secondary" onClick={onClose}>
            {t.page.cancel}
          </Button>
          <Button type="submit" form="rename-folder-dialog-form" loading={rename.isPending} data-action="confirm-rename">
            {t.page.renameSubmit}
          </Button>
        </>
      }
    >
      <form id="rename-folder-dialog-form" onSubmit={submit} className="space-y-3" noValidate>
        {rename.error && <ErrorBanner>{rename.error.message}</ErrorBanner>}
        <Field
          label={t.page.title}
          value={title}
          maxLength={PAGE_TITLE_MAX_LENGTH}
          onChange={(event) => setTitle(event.target.value)}
          error={error}
          autoFocus
        />
      </form>
    </Dialog>
  );
}
