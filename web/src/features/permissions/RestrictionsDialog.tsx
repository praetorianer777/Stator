import { useState } from "react";
import { useMe } from "@/api/auth";
import { ApiError } from "@/api/client";
import type { Page } from "@/api/pages";
import { subjectKey, usePageRestrictions, useSetPageRestrictions, type InheritedRestriction, type Restrictions, type Subject } from "@/api/permissions";
import { Button, Dialog, ErrorBanner, Skeleton } from "@/components/ui";
import { PageLink } from "@/features/pages/PageLink";
import { t } from "@/i18n";
import { SubjectList } from "./SubjectList";
import { SubjectPicker } from "./SubjectPicker";

/** Why the API refused a save, in a sentence that says what to do; a lock-out gets its own. */
export function restrictionsError(error: Error): string {
  if (error instanceof ApiError && error.status === 409) return t.restrictions.lockedOut;
  return error.message;
}

/**
 * Who may view and edit a page: its own two lists, and the lists of every
 * restricted page above it, which it inherits. Changes wait for Save.
 */
export function RestrictionsDialog({ page, spaceKey, onClose }: { page: Page; spaceKey: string; onClose: () => void }) {
  const { data, error, refetch } = usePageRestrictions(page.id);
  return (
    <Dialog title={t.restrictions.title(page.title)} wide onClose={onClose} data-restrictions-dialog={page.id}>
      {error ? (
        <ErrorBanner onRetry={() => void refetch()}>{error.message}</ErrorBanner>
      ) : !data ? (
        <Skeleton />
      ) : (
        <Lists page={page} spaceKey={spaceKey} saved={data} onClose={onClose} />
      )}
    </Dialog>
  );
}

function Lists({ page, spaceKey, saved, onClose }: { page: Page; spaceKey: string; saved: Restrictions; onClose: () => void }) {
  const { data: me } = useMe();
  const save = useSetPageRestrictions(page.id);
  const [view, setView] = useState<Subject[]>(saved.view);
  const [edit, setEdit] = useState<Subject[]>(saved.edit);
  const editable = page.can.restrict;
  const fields = save.error instanceof ApiError ? save.error.fields : {};
  const self: Subject | undefined = me ? { type: "user", id: me.user.id, name: me.user.name || me.user.email } : undefined;

  const change = (setter: (next: Subject[]) => void, next: Subject[]) => {
    save.reset();
    setter(next);
  };

  return (
    <div className="space-y-4" data-restrictions>
      <p className="text-sm text-ink-muted">{t.restrictions.intro}</p>
      <Inherited spaceKey={spaceKey} inherited={saved.inherited} />
      <div className="grid gap-4 sm:grid-cols-2">
        <ListEditor
          kind="view"
          title={t.restrictions.viewTitle}
          hint={t.restrictions.viewHint}
          open={t.restrictions.viewOpen}
          subjects={view}
          onChange={(next) => change(setView, next)}
          editable={editable && !page.home}
          blocked={page.home ? t.restrictions.homeNoView : undefined}
          self={self}
          error={fields.view}
        />
        <ListEditor
          kind="edit"
          title={t.restrictions.editTitle}
          hint={t.restrictions.editHint}
          open={t.restrictions.editOpen}
          subjects={edit}
          onChange={(next) => change(setEdit, next)}
          editable={editable}
          self={self}
          error={fields.edit}
        />
      </div>
      <p className="text-xs text-ink-subtle">{t.restrictions.adminNote}</p>
      {!editable && <p className="text-sm text-ink-muted">{t.restrictions.readOnly}</p>}
      {save.error && !fields.view && !fields.edit && <ErrorBanner>{restrictionsError(save.error)}</ErrorBanner>}
      <div className="flex justify-end gap-2">
        <Button variant="secondary" onClick={onClose}>
          {editable ? t.restrictions.cancel : t.restrictions.close}
        </Button>
        {editable && (
          <Button loading={save.isPending} onClick={() => save.mutate({ view, edit }, { onSuccess: onClose })} data-action="save-restrictions">
            {t.restrictions.save}
          </Button>
        )}
      </div>
    </div>
  );
}

function ListEditor({
  kind,
  title,
  hint,
  open,
  subjects,
  onChange,
  editable,
  blocked,
  self,
  error,
}: {
  kind: "view" | "edit";
  title: string;
  hint: string;
  open: string;
  subjects: Subject[];
  onChange: (next: Subject[]) => void;
  editable: boolean;
  blocked?: string;
  self?: Subject;
  error?: string;
}) {
  const keys = subjects.map(subjectKey);
  // Somebody narrowing a list almost always means to stay on it, so the way to do that sits beside it.
  const offerSelf = editable && self && subjects.length > 0 && !keys.includes(subjectKey(self));
  return (
    <section className="space-y-2 rounded-control border border-border p-3" aria-label={title} data-restriction-list={kind}>
      <h3 className="text-sm font-semibold text-ink">{title}</h3>
      {blocked ? (
        <p className="text-sm text-ink-muted" data-home-no-view>
          {blocked}
        </p>
      ) : (
        <>
          <p className="text-xs text-ink-subtle">{hint}</p>
          <SubjectList
            subjects={subjects}
            empty={open}
            selfId={self?.id ?? undefined}
            onRemove={editable ? (gone) => onChange(subjects.filter((each) => subjectKey(each) !== subjectKey(gone))) : undefined}
          />
          {offerSelf && (
            <Button size="sm" variant="secondary" onClick={() => onChange([...subjects, self])} data-action={`add-me-${kind}`}>
              {t.restrictions.addMe}
            </Button>
          )}
          {editable && <SubjectPicker label={t.permissions.pickerLabel} exclude={keys} onPick={(picked) => onChange([...subjects, picked])} />}
          {error && <p className="text-sm text-danger">{error}</p>}
        </>
      )}
    </section>
  );
}

function Inherited({ spaceKey, inherited }: { spaceKey: string; inherited: InheritedRestriction[] }) {
  return (
    <section className="space-y-2" aria-label={t.restrictions.inheritedTitle} data-inherited>
      <h3 className="text-sm font-semibold text-ink">{t.restrictions.inheritedTitle}</h3>
      {inherited.length === 0 ? (
        <p className="text-sm text-ink-subtle">{t.restrictions.inheritedNone}</p>
      ) : (
        <ul className="space-y-2">
          {inherited.map((above) => (
            <li key={above.page.id} className="space-y-1.5 rounded-control border border-border bg-surface-raised p-3" data-inherited-from={above.page.title}>
              <p className="text-sm font-medium text-ink">
                <PageLink
                  spaceKey={spaceKey}
                  id={above.page.id}
                  title={above.page.title}
                  home={above.page.home}
                  className="text-accent underline underline-offset-2"
                >
                  {t.restrictions.inheritedFrom(above.page.title)}
                </PageLink>
              </p>
              <div className="grid gap-1 text-sm sm:grid-cols-[4rem_1fr]">
                <span className="text-ink-subtle">{t.restrictions.inheritedView}</span>
                <SubjectList subjects={above.view} empty={t.restrictions.inheritedOpen} />
                <span className="text-ink-subtle">{t.restrictions.inheritedEdit}</span>
                <SubjectList subjects={above.edit} empty={t.restrictions.inheritedOpen} />
              </div>
            </li>
          ))}
        </ul>
      )}
    </section>
  );
}
