import { useEffect, useRef, useState, type FormEvent } from "react";
import { useQueryClient } from "@tanstack/react-query";
import { useNavigate } from "@tanstack/react-router";
import { ApiError } from "@/api/client";
import { pageQuery, usePage, type Page } from "@/api/pages";
import type { Space } from "@/api/spaces";
import { useDiscardDraft, useDraft, usePublish, useSaveDraft, type Draft, type PublishOptions } from "@/api/versions";
import { Button, ErrorBanner, Field, PageHeader, Skeleton } from "@/components/ui";
import { Editor } from "@/features/editor/Editor";
import { emptyDoc, type Doc } from "@/features/editor/schema";
import { DRAFT_AUTOSAVE_MS, PAGE_TITLE_MAX_LENGTH } from "@/config";
import { t } from "@/i18n";
import { pageSlug } from "@/lib/slug";
import { pageCrumbs } from "./PageScreen";
import { PublishDialog } from "./PublishDialog";

/**
 * Edits a page into the caller's private draft, saved as they type, and
 * publishes it as the next version when they say so.
 */
export function PageEditor({ pageId }: { pageId: string }) {
  const page = usePage(pageId);
  const draft = useDraft(pageId);
  if (page.data && !page.data.page.can.edit) return <ErrorBanner>{t.page.cannotEdit}</ErrorBanner>;
  const error = page.error ?? draft.error;
  if (error)
    return (
      <ErrorBanner
        onRetry={() => {
          void page.refetch();
          void draft.refetch();
        }}
      >
        {error.message}
      </ErrorBanner>
    );
  if (!page.data || draft.data === undefined) return <Skeleton />;
  // Keyed by the page alone: the form holds what is typed, and a refetch of
  // the page after a conflict must not start it afresh.
  return <PageForm key={page.data.page.id} page={page.data.page} space={page.data.space} draft={draft.data} />;
}

// The header's buttons are drawn in the shell's strip, outside the form, so
// they name it.
const PAGE_FORM_ID = "page-form";

type SaveState = "idle" | "pending" | "saving" | "saved" | "error" | "untitled";

function PageForm({ page, space, draft }: { page: Page; space: Space; draft: Draft | null }) {
  const navigate = useNavigate();
  const queryClient = useQueryClient();
  const save = useSaveDraft(page.id);
  const discard = useDiscardDraft(page.id);
  const publish = usePublish(page.id);
  const [title, setTitle] = useState(draft?.title ?? page.title);
  const [initialBody] = useState<Doc>(draft?.body ?? page.body);
  const [body, setBody] = useState<Doc | null>(initialBody);
  const [base, setBase] = useState(draft?.baseVersion ?? page.version);
  const [hasDraft, setHasDraft] = useState(draft !== null);
  const [state, setState] = useState<SaveState>("idle");
  const [saveError, setSaveError] = useState("");
  const [titleError, setTitleError] = useState("");
  const [dialog, setDialog] = useState(false);
  const [conflict, setConflict] = useState<{ latest: number; options: PublishOptions } | null>(null);

  // The timer and the save chain outlive renders, and the last save has to
  // run even as the editor goes, so they live in refs and read the latest.
  const latest = useRef({ title, body, base });
  latest.current = { title, body, base };
  const saveRef = useRef(save.mutateAsync);
  saveRef.current = save.mutateAsync;
  const dirty = useRef(false);
  const timer = useRef<ReturnType<typeof setTimeout>>(undefined);
  // Saves run one after another, so an older one can never land last.
  const chain = useRef<Promise<boolean>>(Promise.resolve(true));

  function flush(): Promise<boolean> {
    clearTimeout(timer.current);
    timer.current = undefined;
    if (!dirty.current) return chain.current;
    const input = latest.current;
    if (!input.title.trim()) {
      setState("untitled");
      return Promise.resolve(false);
    }
    dirty.current = false;
    setState("saving");
    const run = chain.current.then(() =>
      saveRef.current({ title: input.title, body: input.body ?? emptyDoc, baseVersion: input.base }).then(
        () => {
          setHasDraft(true);
          if (!dirty.current) setState("saved");
          return true;
        },
        (error: Error) => {
          dirty.current = true;
          setSaveError(error.message);
          setState("error");
          return false;
        },
      ),
    );
    chain.current = run;
    return run;
  }

  function changed() {
    dirty.current = true;
    setState("pending");
    clearTimeout(timer.current);
    timer.current = setTimeout(() => void flush(), DRAFT_AUTOSAVE_MS);
  }

  const flushRef = useRef(flush);
  flushRef.current = flush;
  useEffect(() => () => void flushRef.current(), []);

  const unsaved = state === "pending" || state === "saving" || state === "error" || state === "untitled";
  useEffect(() => {
    if (!unsaved) return;
    const warn = (event: BeforeUnloadEvent) => event.preventDefault();
    window.addEventListener("beforeunload", warn);
    return () => window.removeEventListener("beforeunload", warn);
  }, [unsaved]);

  const view = (shown: Page) =>
    shown.home
      ? navigate({ to: "/s/$spaceKey", params: { spaceKey: space.key } })
      : navigate({ to: "/s/$spaceKey/p/$pageId/$slug", params: { spaceKey: space.key, pageId: shown.id, slug: pageSlug(shown.title) } });

  const canPublish = hasDraft || unsaved || page.unpublished;

  function openPublish(event?: FormEvent) {
    event?.preventDefault();
    if (!title.trim()) {
      setTitleError(t.page.emptyTitle);
      return;
    }
    setTitleError("");
    if (canPublish) setDialog(true);
  }

  function publishWith(options: PublishOptions) {
    publish.mutate(options, {
      onSuccess: (saved) => {
        setDialog(false);
        void view(saved);
      },
      onError: async (error) => {
        setDialog(false);
        if (error instanceof ApiError && error.code === "publish_conflict") {
          const fresh = await queryClient.fetchQuery({ ...pageQuery(page.id), staleTime: 0 });
          setConflict({ latest: fresh.page.version, options });
        }
      },
    });
  }

  async function onPublish(options: PublishOptions) {
    setConflict(null);
    if (await flush()) publishWith(options);
    else setDialog(false);
  }

  // Taking the other publish as the base is what lets the draft go over it.
  async function keepAndPublish() {
    if (!conflict) return;
    const { latest: version, options } = conflict;
    clearTimeout(timer.current);
    dirty.current = false;
    await chain.current;
    setBase(version);
    latest.current = { ...latest.current, base: version };
    dirty.current = true;
    if (!(await flush())) return;
    setConflict(null);
    publishWith(options);
  }

  async function discardDraft() {
    if (!window.confirm(page.unpublished ? t.draft.confirmDiscardUnpublished : t.draft.confirmDiscard)) return;
    clearTimeout(timer.current);
    dirty.current = false;
    await chain.current;
    discard.mutate(undefined, { onSuccess: () => void view(page) });
  }

  const status = {
    idle: canPublish ? "" : t.draft.nothingToPublish,
    pending: "",
    saving: t.draft.saving,
    saved: t.draft.saved,
    error: "",
    untitled: t.draft.titleNeeded,
  }[state];

  const error = publish.error && !(publish.error instanceof ApiError && publish.error.code === "publish_conflict") ? publish.error : discard.error;

  return (
    <form id={PAGE_FORM_ID} onSubmit={openPublish} className="mx-auto max-w-3xl space-y-4" data-page-editor={page.id}>
      <PageHeader
        crumbs={pageCrumbs(space, page)}
        title={t.page.editing(page.title)}
        meta={
          <span role="status" data-draft-status={state}>
            {status}
          </span>
        }
        actions={
          <>
            {(hasDraft || unsaved) && (
              <Button type="button" variant="ghost" onClick={() => void discardDraft()} loading={discard.isPending} data-action="discard-draft">
                {t.draft.discard}
              </Button>
            )}
            <Button type="button" variant="secondary" onClick={() => void view(page)} data-action="close-editor">
              {t.draft.close}
            </Button>
            <Button type="submit" form={PAGE_FORM_ID} disabled={!canPublish} loading={publish.isPending} data-action="publish">
              {t.draft.publish}
            </Button>
          </>
        }
      />
      {state === "error" && <ErrorBanner>{t.draft.notSaved(saveError)}</ErrorBanner>}
      {error && <ErrorBanner>{error.message}</ErrorBanner>}
      {conflict && (
        <div role="alert" className="space-y-2 rounded-control border border-warning bg-warning-subtle px-3 py-2 text-sm text-ink" data-publish-conflict="">
          <p className="font-medium">{t.draft.conflictTitle}</p>
          <p>{t.draft.conflictBody(conflict.latest)}</p>
          <div className="flex flex-wrap gap-2 pt-1">
            <Button
              type="button"
              variant="secondary"
              size="sm"
              onClick={() =>
                void navigate({
                  to: "/s/$spaceKey/p/$pageId/$slug/history",
                  params: { spaceKey: space.key, pageId: page.id, slug: pageSlug(page.title) },
                  search: { from: conflict.latest, to: "draft" },
                })
              }
              data-action="compare-conflict"
            >
              {t.draft.compareConflict(conflict.latest)}
            </Button>
            <Button type="button" size="sm" onClick={() => void keepAndPublish()} loading={publish.isPending} data-action="keep-and-publish">
              {t.draft.keepAndPublish}
            </Button>
            <Button type="button" variant="danger" size="sm" onClick={() => void discardDraft()} data-action="discard-conflict">
              {t.draft.discard}
            </Button>
          </div>
        </div>
      )}
      <Field
        label={t.page.title}
        value={title}
        maxLength={PAGE_TITLE_MAX_LENGTH}
        onChange={(event) => {
          setTitle(event.target.value);
          changed();
        }}
        error={titleError}
        controlSize="lg"
      />
      <Editor
        id="page-body"
        value={initialBody}
        onChange={(doc) => {
          setBody(doc);
          changed();
        }}
        onSubmit={() => openPublish()}
      />
      {dialog && <PublishDialog title={title} busy={publish.isPending} onClose={() => setDialog(false)} onPublish={(options) => void onPublish(options)} />}
    </form>
  );
}
