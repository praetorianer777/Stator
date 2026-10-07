import { useContext, useState } from "react";
import { useEditorState, type Editor } from "@tiptap/react";
import { NodeSelection } from "@tiptap/pm/state";
import { useAttachments, type Attachment } from "@/api/attachments";
import { Button, Field, IconButton, SelectInput } from "@/components/ui";
import { Icon } from "@/components/icons";
import { IMAGE_ALT_MAX_LENGTH, IMAGE_WIDTHS } from "@/config";
import { t } from "@/i18n";
import { AnnotateDialog } from "@/features/annotate/AnnotateDialog";
import { isAnnotatable } from "@/features/annotate/annotation";
import { DocPageContext } from "./BlockViews";
import { RovingToolbar } from "./RovingToolbar";
import { imageAlt } from "./attachments";

type WidthName = keyof typeof IMAGE_WIDTHS;
const WIDTH_NAMES = Object.keys(IMAGE_WIDTHS) as WidthName[];

function widthName(width: unknown): WidthName | "" {
  return WIDTH_NAMES.find((name) => IMAGE_WIDTHS[name] === (width ?? null)) ?? "";
}

/** Whether the editor was given somewhere to send files. */
export function takesFiles(editor: Editor): boolean {
  return editor.extensionManager.extensions.some((ext) => ext.name === "fileUpload" && Boolean(ext.options.upload));
}

// Made on demand rather than drawn, because an input in the toolbar would be
// one more stop for its arrow keys.
function pickFiles(onPick: (files: File[]) => void) {
  const input = document.createElement("input");
  input.type = "file";
  input.multiple = true;
  input.addEventListener("change", () => onPick([...(input.files ?? [])]));
  input.click();
}

/** The toolbar's button that opens the file picker; what is picked goes where the caret is. */
export function AttachButton({ editor }: { editor: Editor }) {
  if (!takesFiles(editor)) return null;
  return (
    <IconButton
      icon={<Icon.Paperclip />}
      label={t.attachments.attach}
      size="sm"
      data-editor-action="attach"
      onMouseDown={(e) => e.preventDefault()}
      onClick={() => pickFiles((files) => editor.chain().focus().uploadFiles(files).run())}
    />
  );
}

/** Points the picture drawn on at its edited version, where it still is or wherever it went. */
function showEdited(editor: Editor, pos: number, from: string, to: string) {
  const { doc } = editor.state;
  let target = doc.nodeAt(pos)?.type.name === "image" && doc.nodeAt(pos)?.attrs.attachmentId === from ? pos : -1;
  if (target < 0)
    doc.descendants((node, at) => {
      if (target < 0 && node.type.name === "image" && node.attrs.attachmentId === from) target = at;
      return target < 0;
    });
  if (target >= 0) editor.chain().setNodeSelection(target).updateAttributes("image", { attachmentId: to }).run();
}

/** The toolbar's way into annotating the selected picture, once the page's files say it is one that can be. */
function AnnotateImageButton({ pageId, attachmentId, onOpen }: { pageId: string; attachmentId: string | null; onOpen: (file: Attachment) => void }) {
  const file = useAttachments(pageId).data?.find((each) => each.id === attachmentId);
  if (!file || !isAnnotatable(file.contentType)) return null;
  return (
    <Button
      variant="ghost"
      size="sm"
      icon={<Icon.Edit />}
      onMouseDown={(e) => e.preventDefault()}
      onClick={() => onOpen(file)}
      data-editor-action="annotate-image"
    >
      {t.annotate.action}
    </Button>
  );
}

/** Alternative text and width of the selected image, a way to annotate it, and a way to take it out. */
export function ImageTools({ editor }: { editor: Editor }) {
  const image = useEditorState({
    editor,
    selector: ({ editor: e }) => {
      const { selection } = e.state;
      if (!(selection instanceof NodeSelection) || selection.node.type.name !== "image") return null;
      return {
        pos: selection.from,
        attachmentId: selection.node.attrs.attachmentId as string | null,
        alt: (selection.node.attrs.alt as string | null) ?? "",
        width: selection.node.attrs.width as number | null,
      };
    },
  });
  const page = useContext(DocPageContext);
  const [annotating, setAnnotating] = useState<{ file: Attachment; pos: number } | null>(null);
  // Outside the toolbar row, so it stays open while the editor's selection moves.
  const dialog = annotating && (
    <AnnotateDialog
      file={annotating.file}
      offerInPage
      onClose={() => setAnnotating(null)}
      onSaved={(made, showInPage) => {
        setAnnotating(null);
        if (showInPage) showEdited(editor, annotating.pos, annotating.file.id, made.id);
      }}
    />
  );
  if (!image) return dialog || null;
  const set = (attrs: Record<string, unknown>) => editor.chain().setNodeSelection(image.pos).updateAttributes("image", attrs).run();
  return (
    <div className="flex flex-wrap items-end gap-2 px-1 py-1" data-editor-tools="image">
      <Field
        key={image.pos}
        label={t.attachments.altText}
        id="editor-image-alt"
        defaultValue={image.alt}
        maxLength={IMAGE_ALT_MAX_LENGTH}
        hint={t.attachments.altHint}
        className="w-72"
        onChange={(e) => set({ alt: imageAlt(e.target.value) })}
        data-editor-action="image-alt"
      />
      <RovingToolbar label={t.attachments.imageTools}>
        <SelectInput
          controlSize="sm"
          aria-label={t.attachments.width}
          value={widthName(image.width)}
          onChange={(e) => set({ width: IMAGE_WIDTHS[e.target.value as WidthName] ?? null })}
          data-editor-action="image-width"
        >
          {widthName(image.width) === "" && <option value="">{`${image.width} px`}</option>}
          {WIDTH_NAMES.map((name) => (
            <option key={name} value={name}>
              {t.attachments.widths[name]}
            </option>
          ))}
        </SelectInput>
        {page && takesFiles(editor) && (
          <AnnotateImageButton pageId={page.id} attachmentId={image.attachmentId} onOpen={(file) => setAnnotating({ file, pos: image.pos })} />
        )}
        <Button
          variant="ghost"
          size="sm"
          onMouseDown={(e) => e.preventDefault()}
          onClick={() => editor.chain().focus().setNodeSelection(image.pos).deleteSelection().run()}
          data-editor-action="remove-image"
        >
          {t.attachments.removeImage}
        </Button>
      </RovingToolbar>
      {dialog}
    </div>
  );
}
