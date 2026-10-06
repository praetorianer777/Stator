import { useCallback, useEffect, useMemo, useRef, useState, type FormEvent } from "react";
import { useQueryClient } from "@tanstack/react-query";
import { useNavigate } from "@tanstack/react-router";
import { ApiError } from "@/api/client";
import { useMe } from "@/api/auth";
import { useMentionSource } from "@/api/mentions";
import { pageQuery, usePage, type Page } from "@/api/pages";
import type { Space } from "@/api/spaces";
import { useDiscardDraft, useDraft, usePublish, useSaveDraft, useSaveLive, type Draft, type PublishOptions } from "@/api/versions";
import { Button, ErrorBanner, Field, PageHeader, Skeleton, Tag, cx } from "@/components/ui";
import { useEditorAttachments } from "@/features/attachments/hooks";
import { ArmatureIssuesProvider } from "@/features/armature/IssueChip";
import { issueKeysOf } from "@/features/armature/issueKeys";
import { useIssueSource } from "@/features/armature/useIssueSource";
import { Presence } from "@/features/collab/Presence";
import type { CollabSession, CollabSnapshot, CollabUser, SeedContent } from "@/features/collab/session";
import { colorFor, setSharedTitle, titleOf } from "@/features/collab/shared";
import { useCollab } from "@/features/collab/useCollab";
import { DocPageContext } from "@/features/editor/BlockViews";
import { Editor } from "@/features/editor/Editor";
import { emptyDoc, type Doc } from "@/features/editor/schema";
import { fillSharedDraft, sharedBody } from "@/features/editor/sharedDraft";
import { DRAFT_AUTOSAVE_MS, PAGE_TITLE_MAX_LENGTH } from "@/config";
import { t } from "@/i18n";
import { pageSlug } from "@/lib/slug";
import { pageCrumbs } from "./PageScreen";
import { PAGE_SHEET_HEADER, pageSheet } from "./pageSheet";
import { PublishDialog } from "./PublishDialog";

/**
 * Edits a page, together with everybody else editing it when the shared
 * draft is in reach and alone otherwise. What is typed is saved into the
 * caller's own draft, which publishing publishes, or for a live page into the page.
 */
export function PageEditor({ pageId }: { pageId: string }) {
  const page = usePage(pageId);
  const draft = useDraft(pageId);
  if (page.data && !page.data.page.can.edit) {
    return <ErrorBanner>{page.data.page.archived ? t.page.cannotEditArchived : t.page.cannotEdit}</ErrorBanner>;
  }
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
  return <PageSession key={page.data.page.id} page={page.data.page} space={page.data.space} draft={draft.data} />;
}

/** The shared draft as the form uses it. */
interface Together {
  session: CollabSession;
  snapshot: CollabSnapshot;
  user: CollabUser;
}

/** Finds out whether the page can be edited together before the form starts. */
function PageSession({ page, space, draft }: { page: Page; space: Space; draft: Draft | null }) {
  const queryClient = useQueryClient();
  const me = useMe().data?.user;
  const user = useMemo<CollabUser | null>(() => (me ? { id: me.id, name: me.name, color: colorFor(me.id) } : null), [me]);
  // The first person in seeds the shared draft with their own draft, as
  // editing alone would open it; after the draft was thrown away, with the
  // page as it now stands. A live page has no drafts, only itself.
  const seed = useCallback(
    async (afresh: boolean): Promise<SeedContent> => {
      const own = page.mode === "live" ? null : draft;
      let from = { title: own?.title ?? page.title, body: own?.body ?? page.body, base: own?.baseVersion ?? page.version };
      if (afresh || page.mode === "live") {
        const fresh = await queryClient.fetchQuery({ ...pageQuery(page.id), staleTime: 0 });
        from = { title: fresh.page.title, body: fresh.page.body, base: fresh.page.version };
      }
      return { base: from.base, fill: (doc) => fillSharedDraft(doc, from.title, from.body) };
    },
    [draft, page, queryClient],
  );
  const { mode, snapshot, session } = useCollab({ pageId: page.id, user, seed });
  if (mode === "connecting") return <Skeleton />;
  const together = mode === "collab" && session && user ? { session, snapshot, user } : null;
  return <PageForm key={mode} page={page} space={space} draft={draft} together={together} />;
}

// The header's buttons are drawn in the shell's strip, outside the form, so
// they name it.
const PAGE_FORM_ID = "page-form";

type SaveState = "idle" | "pending" | "saving" | "saved" | "error" | "untitled";

function PageForm({ page, space, draft, together }: { page: Page; space: Space; draft: Draft | null; together: Together | null }) {
  const navigate = useNavigate();
  const sheet = pageSheet(page.appearance.width);
  const queryClient = useQueryClient();
  const save = useSaveDraft(page.id);
  const saveLive = useSaveLive(page.id);
  const liveMode = page.mode === "live";
  const discard = useDiscardDraft(page.id);
  const publish = usePublish(page.id);
  const doc = together?.snapshot.doc ?? null;
  const live = together?.snapshot.status === "live";
  const stopped = together?.snapshot.stopped ?? null;
  const [title, setTitle] = useState(() => (doc ? titleOf(doc).toString() : (draft?.title ?? page.title)));
  const [initialBody] = useState<Doc>(draft?.body ?? page.body);
  const [body, setBody] = useState<Doc | null>(() => (doc ? sharedBody(doc) : initialBody));
  const [base, setBase] = useState(together ? together.snapshot.base : (draft?.baseVersion ?? page.version));
  const [hasDraft, setHasDraft] = useState(draft !== null);
  const [state, setState] = useState<SaveState>("idle");
  const [saveError, setSaveError] = useState("");
  const [titleError, setTitleError] = useState("");
  const files = useEditorAttachments(page.id);
  const mentionSource = useMentionSource(page.id);
  const armature = useIssueSource(page.id);
  const issueKeys = useMemo(() => issueKeysOf(body), [body]);
  const [dialog, setDialog] = useState(false);
  const [conflict, setConflict] = useState<{ latest: number; options: PublishOptions } | null>(null);

  // The timer and the save chain outlive renders, and the last save has to
  // run even as the editor goes, so they live in refs and read the latest.
  const room = together?.snapshot.room ?? undefined;
  const latest = useRef({ title, body, base, room });
  latest.current = { title, body, base, room };
  const saveRef = useRef(save.mutateAsync);
  saveRef.current = save.mutateAsync;
  const saveLiveRef = useRef(saveLive.mutateAsync);
  saveLiveRef.current = saveLive.mutateAsync;
  const dirty = useRef(false);
  const timer = useRef<ReturnType<typeof setTimeout>>(undefined);
  // Offline, a save would only fail; the shared draft keeps the words until
  // the connection returns, and the save follows then.
  const offline = useRef(false);
  offline.current = together !== null && !live;
  // Saves run one after another, so an older one can never land last.
  const chain = useRef<Promise<boolean>>(Promise.resolve(true));
  // Only the last save queued speaks for the draft: an older one that ends
  // while a newer one waits must neither call it saved nor mark it unsaved.
  const queued = useRef(0);
  // The save flushed as the editor goes still lands in the cache, but what it
  // says about itself has no form left to show it in.
  const mounted = useRef(false);
  const report = (update: () => void) => {
    if (mounted.current) update();
  };

  function flush(): Promise<boolean> {
    clearTimeout(timer.current);
    timer.current = undefined;
    if (!dirty.current) return chain.current;
    const input = latest.current;
    if (!input.title.trim()) {
      report(() => setState("untitled"));
      return Promise.resolve(false);
    }
    dirty.current = false;
    report(() => setState("saving"));
    const turn = ++queued.current;
    const write = liveMode
      ? () => saveLiveRef.current({ title: input.title, body: input.body ?? emptyDoc, room: input.room })
      : () => saveRef.current({ title: input.title, body: input.body ?? emptyDoc, baseVersion: input.base });
    const run = chain.current.then(() =>
      write().then(
        () => {
          report(() => {
            if (!liveMode) setHasDraft(true);
            if (turn === queued.current && !dirty.current) setState("saved");
          });
          return true;
        },
        (error: Error) => {
          if (turn !== queued.current) return false;
          dirty.current = true;
          report(() => {
            setSaveError(error.message);
            setState("error");
          });
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
    if (!offline.current) timer.current = setTimeout(() => void flush(), DRAFT_AUTOSAVE_MS);
  }

  const flushRef = useRef(flush);
  flushRef.current = flush;
  useEffect(() => {
    mounted.current = true;
    return () => {
      mounted.current = false;
      if (!offline.current) void flushRef.current();
    };
  }, []);

  // Back online, what was written meanwhile is saved.
  useEffect(() => {
    if (live && dirty.current) void flushRef.current();
  }, [live]);

  // A publish from the shared draft, here or in another browser, moves
  // everybody's base on.
  const sharedBase = together?.snapshot.base;
  useEffect(() => {
    if (sharedBase !== undefined) setBase(sharedBase);
  }, [sharedBase]);

  // The shared title follows whoever types in it; a new room brings its own.
  useEffect(() => {
    if (!doc) return;
    const text = titleOf(doc);
    const follow = () => setTitle(text.toString());
    follow();
    text.observe(follow);
    return () => text.unobserve(follow);
  }, [doc]);

  const unsaved = state === "pending" || state === "saving" || state === "error" || state === "untitled";
  // Together, the shared draft holds every word as it is typed; only words
  // written offline are not yet anywhere but this browser.
  const atRisk = together ? unsaved && !live : unsaved;
  useEffect(() => {
    if (!atRisk) return;
    const warn = (event: BeforeUnloadEvent) => event.preventDefault();
    window.addEventListener("beforeunload", warn);
    return () => window.removeEventListener("beforeunload", warn);
  }, [atRisk]);

  const view = (shown: Page) =>
    shown.home
      ? navigate({ to: "/s/$spaceKey", params: { spaceKey: space.key } })
      : navigate({ to: "/s/$spaceKey/p/$pageId/$slug", params: { spaceKey: space.key, pageId: shown.id, slug: pageSlug(shown.title) } });

  // Together, somebody else's changes may be waiting to be published even
  // when this person changed nothing.
  const canPublish = together !== null || hasDraft || unsaved || page.unpublished;

  function openPublish(event?: FormEvent) {
    event?.preventDefault();
    if (liveMode) {
      void flush();
      return;
    }
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
        together?.session.published(saved.version);
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
    // Together, the shared draft as it stands now is what is published,
    // whoever wrote the last of it.
    if (together) dirty.current = true;
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
    const question = together ? t.collab.confirmDiscard : page.unpublished ? t.draft.confirmDiscardUnpublished : t.draft.confirmDiscard;
    if (!window.confirm(question)) return;
    clearTimeout(timer.current);
    dirty.current = false;
    await chain.current;
    together?.session.discard();
    discard.mutate(undefined, { onSuccess: () => void view(page) });
  }

  const status = {
    idle: canPublish || liveMode ? "" : t.draft.nothingToPublish,
    pending: "",
    saving: liveMode ? t.live.saving : t.draft.saving,
    saved: liveMode ? t.live.saved : together ? t.collab.saved : t.draft.saved,
    error: "",
    untitled: t.draft.titleNeeded,
  }[state];

  const error = publish.error && !(publish.error instanceof ApiError && publish.error.code === "publish_conflict") ? publish.error : discard.error;

  return (
    <form
      id={PAGE_FORM_ID}
      onSubmit={openPublish}
      className={cx(sheet.className, "space-y-4")}
      style={sheet.style}
      data-page-editor={page.id}
      data-collab={together ? "together" : "alone"}
      data-page-mode={page.mode}
    >
      <PageHeader
        className={PAGE_SHEET_HEADER}
        style={sheet.style}
        crumbs={pageCrumbs(space, page)}
        title={t.page.editing(page.title)}
        meta={
          <span className="inline-flex flex-wrap items-center gap-3">
            {liveMode && <Tag data-live-badge="">{t.live.badge}</Tag>}
            {together && <Presence awareness={together.snapshot.awareness} selfId={together.user.id} status={together.snapshot.status} />}
            <span role="status" data-draft-status={state}>
              {status}
            </span>
          </span>
        }
        actions={
          liveMode ? (
            <Button type="button" onClick={() => void view(page)} data-action="close-editor">
              {t.live.done}
            </Button>
          ) : (
            <>
              {(hasDraft || unsaved || together) && (
                <Button type="button" variant="ghost" onClick={() => void discardDraft()} loading={discard.isPending} data-action="discard-draft">
                  {t.draft.discard}
                </Button>
              )}
              <Button type="button" variant="secondary" onClick={() => void view(page)} data-action="close-editor">
                {t.draft.close}
              </Button>
              <Button type="submit" form={PAGE_FORM_ID} disabled={!canPublish || stopped !== null} loading={publish.isPending} data-action="publish">
                {t.draft.publish}
              </Button>
            </>
          )
        }
      />
      {stopped && <ErrorBanner>{stopped === "signedOut" ? t.collab.signedOut : t.collab.refused}</ErrorBanner>}
      {state === "error" && <ErrorBanner>{liveMode ? t.live.notSaved(saveError) : t.draft.notSaved(saveError)}</ErrorBanner>}
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
        readOnly={stopped !== null}
        onChange={(event) => {
          setTitle(event.target.value);
          if (doc) setSharedTitle(titleOf(doc), event.target.value);
          changed();
        }}
        error={titleError}
        controlSize="lg"
      />
      {files.errors.map((message) => (
        <ErrorBanner key={message}>{message}</ErrorBanner>
      ))}
      <DocPageContext value={{ id: page.id, spaceKey: space.key }}>
        <ArmatureIssuesProvider keys={issueKeys}>
          {together && !(together.snapshot.ready && doc && together.snapshot.awareness) ? (
            <Skeleton rows={6} />
          ) : (
            <Editor
              key={together?.snapshot.room ?? "alone"}
              id="page-body"
              value={together ? null : initialBody}
              collab={together && doc && together.snapshot.awareness ? { doc, awareness: together.snapshot.awareness, user: together.user } : undefined}
              readOnly={stopped !== null}
              taskIds={liveMode}
              onChange={(next, remote) => {
                setBody(next);
                if (!remote) changed();
              }}
              onSubmit={() => openPublish()}
              upload={files.upload}
              attachments={files.index}
              mentionSource={mentionSource}
              armature={armature}
            />
          )}
        </ArmatureIssuesProvider>
      </DocPageContext>
      {dialog && <PublishDialog title={title} busy={publish.isPending} onClose={() => setDialog(false)} onPublish={(options) => void onPublish(options)} />}
    </form>
  );
}
