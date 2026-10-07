import { useMemo, useState } from "react";
import { useMe } from "@/api/auth";
import { ApiError } from "@/api/client";
import type { Page } from "@/api/pages";
import { subjectKey, subjectRef, type Subject } from "@/api/permissions";
import { shareSearch, useSharePage, useViewers, type Share } from "@/api/sharing";
import { Icon } from "@/components/icons";
import { Button, Dialog, ErrorBanner, Field, Skeleton } from "@/components/ui";
import { SubjectGlyph, SubjectPicker } from "@/features/permissions/SubjectPicker";
import { SHARE_MAX_RECIPIENTS, SHARE_MESSAGE_MAX_LENGTH } from "@/config";
import { t } from "@/i18n";
import { PublicLinks } from "./PublicLinks";

interface Picked {
  subject: Subject;
  detail: string;
  /** Set when the page is closed to the person, or to every member of the group. */
  warning?: string;
}

/** Why the API refused a share, in a sentence that says what to do; a page closed to somebody gets its own. */
export function shareError(error: Error): string {
  if (error instanceof ApiError && error.status === 409 && error.code === "cannot_view") return t.share.refusedClosed;
  return error.message;
}

/**
 * Sends a page to people and groups with a note. Sharing gives nobody
 * access, so the dialog says who may view the page and marks who may not.
 */
export function ShareDialog({ page, onClose }: { page: Page; onClose: () => void }) {
  const [sent, setSent] = useState<Share>();
  return (
    <Dialog title={t.share.title(page.title)} onClose={onClose} data-share-dialog={page.id}>
      {sent ? <Sent share={sent} onClose={onClose} /> : <Compose page={page} onClose={onClose} onSent={setSent} />}
      <div className="mt-4">
        <PublicLinks pageId={page.id} />
      </div>
    </Dialog>
  );
}

function Compose({ page, onClose, onSent }: { page: Page; onClose: () => void; onSent: (share: Share) => void }) {
  const { data: me } = useMe();
  const share = useSharePage(page.id);
  const useSearch = useMemo(() => shareSearch(page.id), [page.id]);
  const [picked, setPicked] = useState<Picked[]>([]);
  const [message, setMessage] = useState("");
  const fields = share.error instanceof ApiError ? share.error.fields : {};
  const keys = picked.map((each) => subjectKey(each.subject));
  const exclude = me ? [...keys, subjectKey({ type: "user", id: me.user.id })] : keys;
  const closed = picked.some((each) => each.warning);
  const full = picked.length >= SHARE_MAX_RECIPIENTS;

  const change = (next: Picked[]) => {
    share.reset();
    setPicked(next);
  };

  return (
    <form
      className="space-y-4"
      onSubmit={(event) => {
        event.preventDefault();
        if (picked.length === 0) return;
        share.mutate({ recipients: picked.map((each) => subjectRef(each.subject)), message: message.trim() }, { onSuccess: onSent });
      }}
      data-share-form
    >
      <p className="text-sm text-ink-muted">{t.share.intro}</p>
      <div className="space-y-2">
        <SubjectPicker
          label={t.share.pickerLabel}
          exclude={exclude}
          useSearch={useSearch}
          disabled={full}
          onPick={(subject, shown) => change([...picked, { subject, ...shown }])}
        />
        {full && <p className="text-xs text-ink-subtle">{t.share.full(SHARE_MAX_RECIPIENTS)}</p>}
        <PickedList picked={picked} onRemove={(gone) => change(picked.filter((each) => subjectKey(each.subject) !== subjectKey(gone)))} />
        {closed && (
          <p className="flex gap-2 rounded-control border border-warning bg-warning-subtle px-3 py-2 text-sm text-ink" data-share-closed-note>
            <Icon.Warning className="mt-0.5 shrink-0" />
            <span>{t.share.closedNote}</span>
          </p>
        )}
        {fields.recipients && <p className="text-sm text-danger">{fields.recipients}</p>}
      </div>
      <Field
        label={t.share.messageLabel}
        hint={t.share.messageHint(SHARE_MESSAGE_MAX_LENGTH)}
        rows={3}
        maxLength={SHARE_MESSAGE_MAX_LENGTH}
        value={message}
        error={fields.message}
        onChange={(event) => {
          share.reset();
          setMessage(event.target.value);
        }}
      />
      <Viewers pageId={page.id} restricted={page.restricted.view} />
      {share.error && !fields.recipients && !fields.message && (
        <ErrorBanner>
          <span data-share-error>{shareError(share.error)}</span>
        </ErrorBanner>
      )}
      <div className="flex justify-end gap-2">
        <Button type="button" variant="secondary" onClick={onClose}>
          {t.share.cancel}
        </Button>
        <Button type="submit" icon={<Icon.Share />} loading={share.isPending} disabled={picked.length === 0} data-action="send-share">
          {t.share.send}
        </Button>
      </div>
    </form>
  );
}

function PickedList({ picked, onRemove }: { picked: Picked[]; onRemove: (subject: Subject) => void }) {
  if (picked.length === 0) return <p className="text-sm text-ink-subtle">{t.share.nobodyYet}</p>;
  return (
    <ul className="space-y-1.5" aria-label={t.share.pickedLabel} data-share-recipients>
      {picked.map(({ subject, detail, warning }) => (
        <li
          key={subjectKey(subject)}
          className="flex min-h-9 items-center gap-2 rounded-control border border-border bg-surface-raised py-1 pr-1 pl-2 text-sm text-ink"
          data-share-recipient={subject.name}
          data-closed={warning ? "" : undefined}
        >
          <SubjectGlyph type={subject.type} />
          <span className="min-w-0 flex-1">
            <span className="font-medium">{subject.name}</span>
            <span className="sr-only">, {subject.type === "group" ? t.permissions.group : t.permissions.person}</span>
            {(warning || subject.type === "group") && (
              <span className={warning ? "block text-xs text-danger" : "block text-xs text-ink-subtle"}>{warning ?? detail}</span>
            )}
          </span>
          <button
            type="button"
            onClick={() => onRemove(subject)}
            aria-label={t.permissions.remove(subject.name)}
            className="inline-flex size-7 shrink-0 items-center justify-center rounded-control text-ink-subtle hover:bg-surface hover:text-ink"
            data-action="remove-recipient"
          >
            <Icon.X />
          </button>
        </li>
      ))}
    </ul>
  );
}

/** Who may already view the page: everybody, or the first few by name and how many more. */
function Viewers({ pageId, restricted }: { pageId: string; restricted: boolean }) {
  const { data, error, refetch } = useViewers(pageId);
  return (
    <section className="space-y-1.5" aria-label={t.share.viewersTitle} data-share-viewers>
      <h3 className="text-sm font-semibold text-ink">{t.share.viewersTitle}</h3>
      {error ? (
        <ErrorBanner onRetry={() => void refetch()}>{error.message}</ErrorBanner>
      ) : !data ? (
        <Skeleton />
      ) : data.everyone ? (
        <p className="text-sm text-ink-muted" data-viewers-everyone>
          {t.share.viewersEveryone}
        </p>
      ) : (
        <>
          <p className="text-sm text-ink-muted" data-viewers-total={data.total}>
            {restricted ? t.share.viewersRestricted(data.total) : t.share.viewersCount(data.total)}
          </p>
          <ul className="flex flex-wrap gap-1.5" data-viewers-list>
            {data.viewers.map((person) => (
              <li
                key={person.id}
                className="inline-flex h-7 items-center gap-1.5 rounded-control border border-border bg-surface-raised px-2 text-sm text-ink"
                data-viewer={person.name || person.email}
              >
                <SubjectGlyph type="user" />
                {person.name || person.email}
              </li>
            ))}
            {data.total > data.viewers.length && (
              <li className="inline-flex h-7 items-center text-sm text-ink-subtle">{t.share.viewersMore(data.total - data.viewers.length)}</li>
            )}
          </ul>
        </>
      )}
    </section>
  );
}

function Sent({ share, onClose }: { share: Share; onClose: () => void }) {
  return (
    <div className="space-y-4">
      <p role="status" className="flex items-center gap-2 text-sm text-ink" data-share-sent={share.people}>
        <Icon.Check className="shrink-0 text-success" />
        {t.share.sent(share.people)}
      </p>
      <div className="flex justify-end">
        <Button onClick={onClose} data-action="close-share">
          {t.share.done}
        </Button>
      </div>
    </div>
  );
}
