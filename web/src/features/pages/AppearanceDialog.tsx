import { useEffect, useId, useState, type ChangeEvent, type KeyboardEvent, type MouseEvent } from "react";
import { attachmentUrl, isImage, useAttachments, useUploadAttachments } from "@/api/attachments";
import { useSetAppearance, type Appearance, type Page } from "@/api/pages";
import { Button, Dialog, ErrorBanner, Input, Labelled } from "@/components/ui";
import { COVER_FOCUS_DEFAULT, COVER_FOCUS_STEP, PAGE_ICON_CHOICES } from "@/config";
import { t } from "@/i18n";
import { loadEmoji, matchEmoji, type Emoji } from "@/lib/emoji";

type Cover = NonNullable<Appearance["cover"]>;

const clampFocus = (n: number) => Math.min(100, Math.max(0, Math.round(n)));

/** Where a cover's focus point goes for a click on its preview, in percent. */
export function focusAt(
  event: Pick<MouseEvent, "clientX" | "clientY">,
  box: Pick<DOMRect, "left" | "top" | "width" | "height">,
): { focusX: number; focusY: number } {
  return {
    focusX: clampFocus(((event.clientX - box.left) / box.width) * 100),
    focusY: clampFocus(((event.clientY - box.top) / box.height) * 100),
  };
}

/** The CSS that keeps a cover's focus point in view however the picture is cut. */
export function coverPosition(cover: Pick<Cover, "focusX" | "focusY">): string {
  return `${cover.focusX}% ${cover.focusY}%`;
}

function IconPicker({ value, onChange }: { value: string | null; onChange: (icon: string | null) => void }) {
  const a = t.appearance;
  const id = useId();
  const [all, setAll] = useState<Emoji[]>([]);
  const [query, setQuery] = useState("");
  useEffect(() => {
    let live = true;
    void loadEmoji().then((list) => live && setAll(list));
    return () => {
      live = false;
    };
  }, []);
  const found = matchEmoji(all, query, PAGE_ICON_CHOICES);
  return (
    <fieldset className="space-y-2">
      <legend className="text-sm font-medium text-ink-muted">{a.icon}</legend>
      <div className="flex items-center gap-2">
        <span className="text-2xl" aria-hidden="true" data-appearance-icon={value ?? ""}>
          {value ?? "·"}
        </span>
        <span className="text-sm text-ink-muted">{value ? a.iconChosen : a.iconNone}</span>
        {value && (
          <Button type="button" size="sm" variant="ghost" onClick={() => onChange(null)} data-action="remove-icon">
            {a.removeIcon}
          </Button>
        )}
      </div>
      <Labelled id={`${id}-find`} label={a.findIcon}>
        <Input id={`${id}-find`} value={query} autoComplete="off" onChange={(event) => setQuery(event.target.value)} />
      </Labelled>
      <div className="flex flex-wrap gap-1" data-appearance-icons="">
        {found.map((each) => (
          <button
            key={each.emoji}
            type="button"
            className="appearance-emoji"
            aria-pressed={value === each.emoji}
            aria-label={a.useIcon(each.names[0] ?? each.description)}
            onClick={() => onChange(each.emoji)}
          >
            {each.emoji}
          </button>
        ))}
      </div>
    </fieldset>
  );
}

function FocusPicker({ cover, onChange }: { cover: Cover; onChange: (cover: Cover) => void }) {
  const a = t.appearance;
  const help = useId();
  const move = (dx: number, dy: number) => onChange({ ...cover, focusX: clampFocus(cover.focusX + dx), focusY: clampFocus(cover.focusY + dy) });
  const onKeyDown = (event: KeyboardEvent<HTMLButtonElement>) => {
    const step = { ArrowLeft: [-1, 0], ArrowRight: [1, 0], ArrowUp: [0, -1], ArrowDown: [0, 1] }[event.key];
    if (!step) return;
    event.preventDefault();
    move(step[0]! * COVER_FOCUS_STEP, step[1]! * COVER_FOCUS_STEP);
  };
  return (
    <div className="space-y-1">
      <p className="text-sm text-ink-muted" id={help}>
        {a.focusHelp}
      </p>
      {/* A button, so the keyboard moves the point with the arrows as the pointer does with a click. */}
      <button
        type="button"
        className="appearance-focus"
        aria-label={a.focus(cover.focusX, cover.focusY)}
        aria-describedby={help}
        onClick={(event) => onChange({ ...cover, ...focusAt(event, event.currentTarget.getBoundingClientRect()) })}
        onKeyDown={onKeyDown}
        data-cover-focus={`${cover.focusX},${cover.focusY}`}
      >
        <img src={attachmentUrl(cover.attachmentId, true)} alt="" />
        <span className="appearance-focus-point" style={{ left: `${cover.focusX}%`, top: `${cover.focusY}%` }} aria-hidden="true" />
      </button>
    </div>
  );
}

/**
 * Chooses how a page looks: an emoji before its title and in the tree, its
 * width, and one of its pictures as its cover with the point that stays in view.
 */
export function AppearanceDialog({ page, onClose }: { page: Page; onClose: () => void }) {
  const a = t.appearance;
  const id = useId();
  const [icon, setIcon] = useState<string | null>(page.appearance.icon ?? null);
  const [width, setWidth] = useState<Appearance["width"]>(page.appearance.width);
  const [cover, setCover] = useState<Cover | null>(page.appearance.cover ?? null);
  const attachments = useAttachments(page.id);
  const images = (attachments.data ?? []).filter((file) => isImage(file.contentType));
  const uploads = useUploadAttachments(page.id);
  const save = useSetAppearance(page.id);

  const choose = (attachmentId: string) =>
    setCover((was) => ({ attachmentId, focusX: was?.focusX ?? COVER_FOCUS_DEFAULT, focusY: was?.focusY ?? COVER_FOCUS_DEFAULT }));
  const upload = async (event: ChangeEvent<HTMLInputElement>) => {
    const file = event.target.files?.[0];
    event.target.value = "";
    if (!file) return;
    const made = await uploads.upload(file);
    if (made) setCover({ attachmentId: made.id, focusX: COVER_FOCUS_DEFAULT, focusY: COVER_FOCUS_DEFAULT });
  };

  return (
    <Dialog title={a.dialog} onClose={onClose} data-appearance-dialog="">
      <form
        className="space-y-4"
        noValidate
        onSubmit={(event) => {
          event.preventDefault();
          save.mutate({ icon, width, cover }, { onSuccess: onClose });
        }}
      >
        {save.error && <ErrorBanner>{save.error.message}</ErrorBanner>}
        {uploads.errors.map((message) => (
          <ErrorBanner key={message}>{message}</ErrorBanner>
        ))}
        <IconPicker value={icon} onChange={setIcon} />
        <fieldset className="space-y-1">
          <legend className="text-sm font-medium text-ink-muted">{a.width}</legend>
          {(["fixed", "full"] as const).map((option) => (
            <label key={option} className="flex items-center gap-2 text-sm text-ink">
              <input
                type="radio"
                name={`${id}-width`}
                checked={width === option}
                onChange={() => setWidth(option)}
                className="accent-accent"
                data-width={option}
              />
              {a.widths[option]}
            </label>
          ))}
        </fieldset>
        <fieldset className="space-y-2">
          <legend className="text-sm font-medium text-ink-muted">{a.cover}</legend>
          <div className="flex flex-wrap gap-2" data-cover-choices="">
            <label className="appearance-cover-choice">
              <input type="radio" name={`${id}-cover`} checked={!cover} onChange={() => setCover(null)} className="accent-accent" />
              {a.noCover}
            </label>
            {images.map((file) => (
              <label key={file.id} className="appearance-cover-choice">
                <input
                  type="radio"
                  name={`${id}-cover`}
                  checked={cover?.attachmentId === file.id}
                  onChange={() => choose(file.id)}
                  className="accent-accent"
                  data-cover-choice={file.id}
                />
                <img src={attachmentUrl(file.id, true)} alt="" />
                <span className="sr-only">{file.fileName}</span>
              </label>
            ))}
          </div>
          <label className="inline-flex cursor-pointer items-center gap-2 text-sm text-accent">
            <input
              type="file"
              accept="image/png,image/jpeg,image/gif,image/webp"
              className="sr-only"
              onChange={(event) => void upload(event)}
              data-cover-upload=""
            />
            {uploads.pending.length > 0 ? a.uploading : a.upload}
          </label>
          {cover && <FocusPicker cover={cover} onChange={setCover} />}
        </fieldset>
        <div className="flex justify-end gap-2 pt-1">
          <Button type="button" variant="secondary" onClick={onClose}>
            {a.cancel}
          </Button>
          <Button type="submit" loading={save.isPending} data-action="save-appearance">
            {a.save}
          </Button>
        </div>
      </form>
    </Dialog>
  );
}
