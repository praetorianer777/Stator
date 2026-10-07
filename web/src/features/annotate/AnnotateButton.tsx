import { useState } from "react";
import type { Attachment } from "@/api/attachments";
import { IconButton } from "@/components/ui";
import { Icon } from "@/components/icons";
import { t } from "@/i18n";
import { AnnotateDialog } from "./AnnotateDialog";
import { isAnnotatable } from "./annotation";

/** A list's button that opens a picture to annotate, for whoever may edit its page; nothing for any other file. */
export function AnnotateButton({ file, label = file.fileName, onSaved }: { file: Attachment; label?: string; onSaved: (made: Attachment) => void }) {
  const [open, setOpen] = useState(false);
  if (!isAnnotatable(file.contentType)) return null;
  return (
    <>
      <IconButton icon={<Icon.Edit />} label={t.annotate.open(label)} size="sm" onClick={() => setOpen(true)} data-action="annotate-attachment" />
      {open && (
        <AnnotateDialog
          file={file}
          onClose={() => setOpen(false)}
          onSaved={(made) => {
            setOpen(false);
            onSaved(made);
          }}
        />
      )}
    </>
  );
}
