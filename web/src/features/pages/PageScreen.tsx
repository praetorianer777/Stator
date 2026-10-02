import { localDateFormat } from "@/lib/format";
import { useState } from "react";
import { Link, useNavigate } from "@tanstack/react-router";
import { useArchivePage } from "@/api/archive";
import { usePage, type Page } from "@/api/pages";
import type { Space } from "@/api/spaces";
import { useVisit } from "@/api/search";
import { useSetTaskDone } from "@/api/tasks";
import { useTrashPage } from "@/api/trash";
import { Button, ErrorBanner, IconButton, Menu, PageHeader, Skeleton, Tag, Tooltip, type Crumb, type MenuItem } from "@/components/ui";
import { Icon } from "@/components/icons";
import { ArchiveBanner, ArchivedMark } from "@/features/archive/ArchiveBanner";
import { ArmatureLinks } from "@/features/armature/ArmatureLinks";
import { AttachmentPanel } from "@/features/attachments/AttachmentPanel";
import { COMMENTS_ID, CommentsSection } from "@/features/comments/CommentsSection";
import { InlineComments } from "@/features/comments/InlineComments";
import { ExportDialog, ImportDialog } from "@/features/markdown/MarkdownDialogs";
import { PageViewsButton, PageViewsDialog } from "@/features/pageviews/PageViews";
import { usePageAttachmentIds } from "@/features/attachments/hooks";
import { KnownAttachmentsContext } from "@/features/editor/attachmentIndex";
import { DocPageContext } from "@/features/editor/BlockViews";
import { DocView } from "@/features/editor/DocView";
import { PageLabels } from "@/features/labels/PageLabels";
import { PageReactions } from "@/features/reactions/Reactions";
import { AccessDialog } from "@/features/permissions/AccessDialog";
import { RestrictionsDialog } from "@/features/permissions/RestrictionsDialog";
import { PageStar } from "@/features/stars/StarButton";
import { ShareDialog } from "@/features/sharing/ShareDialog";
import { StewardshipDialog } from "@/features/stewardship/StewardshipDialog";
import { VerificationBadge } from "@/features/stewardship/VerificationBadge";
import { WatchMenu } from "@/features/watching/WatchMenu";
import { t } from "@/i18n";
import { pageSlug } from "@/lib/slug";
import { NewPageDialog } from "./NewPageDialog";
import { PageLink } from "./PageLink";
import { PlaceDialog } from "./PlaceDialog";

const updatedAt = localDateFormat({ dateStyle: "medium" });

/** The trail above a page's title: the directory, its space by name, then every page above it. */
export function pageCrumbs(space: Space, page: Page): Crumb[] {
  const crumbs: Crumb[] = [{ label: t.spaces.title, render: (label) => <Link to="/spaces">{label}</Link> }];
  for (const above of page.ancestors) {
    crumbs.push({
      label: above.home ? space.name : above.title,
      render: (label) => (
        <PageLink spaceKey={space.key} id={above.id} title={above.title} home={above.home}>
          {label}
        </PageLink>
      ),
    });
  }
  return crumbs;
}

type Dialog = "new" | "move" | "copy" | "restrictions" | "access" | "export" | "import" | "stewardship" | "share" | "views";

/** Says a page is narrowed to some people, and opens who and why. */
function RestrictedBadge({ page, onOpen }: { page: Page; onOpen: () => void }) {
  const why = page.restricted.view ? t.restrictions.indicatorView : t.restrictions.indicatorEdit;
  return (
    <Tooltip text={why}>
      <button
        type="button"
        onClick={onOpen}
        aria-label={`${t.restrictions.indicator}. ${t.restrictions.showWhy}`}
        className="inline-flex h-6 items-center gap-1 rounded-control border border-border bg-surface-raised px-1.5 text-2xs font-medium text-ink-muted hover:border-border-strong hover:text-ink"
        data-page-restricted={page.restricted.view ? "view" : "edit"}
      >
        <Icon.Lock />
        {t.restrictions.indicator}
      </button>
    </Tooltip>
  );
}

/** How many comments are below the page, as a way down to them. */
function CommentCount({ count }: { count: number }) {
  return (
    <a
      href={`#${COMMENTS_ID}`}
      onClick={(event) => {
        event.preventDefault();
        document.getElementById(COMMENTS_ID)?.scrollIntoView({ block: "start" });
      }}
      aria-label={t.comments.countLink(count)}
      className="inline-flex h-6 items-center gap-1 rounded-control px-1 text-ink-muted hover:text-ink hover:underline"
      data-comment-count={count}
    >
      <Icon.Comment />
      {t.comments.count(count)}
    </a>
  );
}

/** A page as a reader sees it: its place, its title, who last changed it, and its document. */
export function PageScreen({ pageId, thread, reviewing = false }: { pageId: string; thread?: string; reviewing?: boolean }) {
  const { data, isLoading, error, refetch } = usePage(pageId);
  const navigate = useNavigate();
  const [dialog, setDialog] = useState<Dialog>();
  const [watchFailed, setWatchFailed] = useState(false);
  const [starFailed, setStarFailed] = useState(false);
  const [taskFailed, setTaskFailed] = useState(false);
  const setTaskDone = useSetTaskDone();
  // A dialog is about the page it was opened on, so going to another page closes it.
  const [dialogPage, setDialogPage] = useState(pageId);
  if (dialogPage !== pageId) {
    setDialogPage(pageId);
    setDialog(undefined);
    setWatchFailed(false);
    setStarFailed(false);
    setTaskFailed(false);
  }
  // Opening a page from the stale report to review it is not reading it, or
  // reviewing the report would empty it.
  useVisit(reviewing ? undefined : data?.page.id);
  const trash = useTrashPage(data?.space.key ?? "");
  const archive = useArchivePage();
  const attachmentIds = usePageAttachmentIds(pageId);
  if (error) return <ErrorBanner onRetry={() => void refetch()}>{error.message}</ErrorBanner>;
  if (isLoading || !data) return <Skeleton />;
  const { page, space } = data;
  // Ticking a box publishes the page, so only an editor of a published page out of the archive gets live boxes.
  const toggleTask =
    page.can.edit && !page.unpublished && !page.archived
      ? (taskId: string, done: boolean) => {
          setTaskFailed(false);
          setTaskDone.mutate({ pageId: page.id, taskId, done }, { onError: () => setTaskFailed(true) });
        }
      : undefined;
  const edit = () => navigate({ to: "/s/$spaceKey/p/$pageId/$slug/edit", params: { spaceKey: space.key, pageId: page.id, slug: pageSlug(page.title) } });
  const history = () => navigate({ to: "/s/$spaceKey/p/$pageId/$slug/history", params: { spaceKey: space.key, pageId: page.id, slug: pageSlug(page.title) } });
  const open = (placed: Page, editing = false) => {
    setDialog(undefined);
    const params = { spaceKey: placed.spaceKey, pageId: placed.id, slug: pageSlug(placed.title) };
    void navigate(editing ? { to: "/s/$spaceKey/p/$pageId/$slug/edit", params } : { to: "/s/$spaceKey/p/$pageId/$slug", params });
  };

  const actions: MenuItem[] = [];
  if (!page.home && page.can.edit) actions.push({ label: t.page.move, onSelect: () => setDialog("move"), attrs: { "data-action": "move-page" } });
  if (space.can.editPages) actions.push({ label: t.page.copy, onSelect: () => setDialog("copy"), attrs: { "data-action": "copy-page" } });
  actions.push({ label: t.markdown.exportMenu, icon: <Icon.Download />, onSelect: () => setDialog("export"), attrs: { "data-action": "export-markdown" } });
  if (page.can.edit) {
    actions.push({ label: t.markdown.importMenu, icon: <Icon.Upload />, onSelect: () => setDialog("import"), attrs: { "data-action": "import-markdown" } });
  }
  if (page.can.restrict) {
    actions.push({ label: t.restrictions.menu, icon: <Icon.Lock />, onSelect: () => setDialog("restrictions"), attrs: { "data-action": "page-restrictions" } });
  }
  if (page.can.edit && !page.unpublished) {
    actions.push({ label: t.stewardship.menu, icon: <Icon.Seal />, onSelect: () => setDialog("stewardship"), attrs: { "data-action": "page-stewardship" } });
  }
  if (space.can.administer) {
    actions.push({ label: t.access.menu, icon: <Icon.Key />, onSelect: () => setDialog("access"), attrs: { "data-action": "inspect-access" } });
  }
  if (page.can.archive && !page.archived) {
    actions.push({
      label: t.archive.menuArchive,
      icon: <Icon.Archive />,
      onSelect: () => {
        if (window.confirm(t.archive.confirmArchive(page.title))) archive.mutate({ id: page.id, archived: true });
      },
      attrs: { "data-action": "archive-page" },
    });
  }
  if (!page.home && page.can.delete) {
    actions.push({
      label: t.page.moveToTrash,
      danger: true,
      onSelect: () => {
        if (!window.confirm(t.page.confirmTrash(page.title))) return;
        // The reader lands on the page it was under, which is where it would come back.
        const above = page.ancestors[page.ancestors.length - 1];
        trash.mutate(page.id, {
          onSuccess: () =>
            void (above && !above.home
              ? navigate({ to: "/s/$spaceKey/p/$pageId/$slug", params: { spaceKey: space.key, pageId: above.id, slug: pageSlug(above.title) } })
              : navigate({ to: "/s/$spaceKey", params: { spaceKey: space.key } })),
        });
      },
      attrs: { "data-action": "trash-page" },
    });
  }

  return (
    <article className="mx-auto max-w-3xl" data-page={page.id} data-page-home={page.home || undefined}>
      <PageHeader
        crumbs={pageCrumbs(space, page)}
        title={
          <>
            <span data-page-title>{page.title}</span>
            {page.unpublished && <Tag data-unpublished="">{t.page.unpublished}</Tag>}
            {page.archived && (
              <>
                {" "}
                <ArchivedMark />
              </>
            )}
          </>
        }
        meta={
          <span className="flex flex-wrap items-center gap-2">
            {t.page.updated(page.updatedByName, updatedAt.format(new Date(page.updatedAt)))}
            {page.verification && <VerificationBadge verification={page.verification} onOpen={() => setDialog("stewardship")} />}
            {page.owner && (
              <span data-page-owner={page.owner.name}>
                {t.stewardship.owner(page.owner.name)}
                {!page.owner.canView && page.can.edit && <span className="text-danger"> ({t.stewardship.ownerNoAccess})</span>}
              </span>
            )}
            {(page.restricted.view || page.restricted.edit) && <RestrictedBadge page={page} onOpen={() => setDialog("restrictions")} />}
            {page.comments.page > 0 && <CommentCount count={page.comments.page} />}
            {!page.unpublished && <PageViewsButton pageId={page.id} onOpen={() => setDialog("views")} />}
          </span>
        }
        actions={
          <>
            <PageStar page={page} space={space} onFailure={setStarFailed} />
            {!page.unpublished && <WatchMenu page={page} space={space} onFailure={setWatchFailed} />}
            {!page.unpublished && (
              <Button variant="secondary" icon={<Icon.Share />} onClick={() => setDialog("share")} data-action="share-page">
                {t.share.button}
              </Button>
            )}
            <Button variant="secondary" onClick={history} data-action="page-history">
              {t.page.history}
            </Button>
            {page.can.edit && (
              <>
                <Button variant="secondary" icon={<Icon.Plus />} onClick={() => setDialog("new")} data-action="new-page">
                  {t.page.newPage}
                </Button>
                <Button variant="secondary" icon={<Icon.Edit />} onClick={edit} data-action="edit-page">
                  {t.page.edit}
                </Button>
              </>
            )}
            {actions.length > 0 && (
              <Menu
                label={t.page.actions}
                align="end"
                items={actions}
                trigger={(props) => (
                  <IconButton
                    icon={<Icon.More />}
                    label={t.page.actions}
                    variant="secondary"
                    onClick={props.toggle}
                    aria-haspopup={props["aria-haspopup"]}
                    aria-expanded={props["aria-expanded"]}
                    aria-controls={props["aria-controls"]}
                    data-action="page-menu"
                  />
                )}
              />
            )}
          </>
        }
      />
      {trash.error && <ErrorBanner>{trash.error.message}</ErrorBanner>}
      {archive.error && <ErrorBanner>{archive.error.message}</ErrorBanner>}
      <ArchiveBanner page={page} space={space} />
      {watchFailed && <ErrorBanner>{t.watch.failed}</ErrorBanner>}
      {starFailed && <ErrorBanner>{t.star.failed}</ErrorBanner>}
      {taskFailed && <ErrorBanner>{t.tasks.tickFailed}</ErrorBanner>}
      {page.unpublished && (
        <p className="mb-4 text-sm text-ink-muted" data-unpublished-note="">
          {t.page.unpublishedNote}
        </p>
      )}
      {page.draft && page.can.edit && (
        <div
          className="mb-4 flex flex-wrap items-center gap-3 rounded-control border border-border bg-surface-raised px-3 py-2 text-sm text-ink"
          data-draft-note=""
        >
          <span className="min-w-0 flex-1">{t.page.draftNote}</span>
          <Button size="sm" variant="secondary" onClick={edit} data-action="continue-draft">
            {t.page.continueDraft}
          </Button>
        </div>
      )}
      <InlineComments
        page={page}
        thread={thread}
        below={
          <>
            <PageReactions page={page} />
            <PageLabels page={page} />
            <ArmatureLinks page={page} />
            <AttachmentPanel pageId={page.id} editable={page.can.edit} />
            <CommentsSection page={page} thread={thread} />
          </>
        }
      >
        <KnownAttachmentsContext value={attachmentIds}>
          <DocPageContext value={{ id: page.id, spaceKey: space.key, onEdit: page.can.edit ? edit : undefined, toggleTask }}>
            <DocView doc={page.body} />
          </DocPageContext>
        </KnownAttachmentsContext>
      </InlineComments>
      {dialog === "restrictions" && <RestrictionsDialog page={page} spaceKey={space.key} onClose={() => setDialog(undefined)} />}
      {dialog === "stewardship" && <StewardshipDialog page={page} onClose={() => setDialog(undefined)} />}
      {dialog === "share" && <ShareDialog page={page} onClose={() => setDialog(undefined)} />}
      {dialog === "views" && <PageViewsDialog pageId={page.id} onClose={() => setDialog(undefined)} />}
      {dialog === "access" && <AccessDialog pageId={page.id} pageTitle={page.title} onClose={() => setDialog(undefined)} />}
      {dialog === "export" && <ExportDialog page={page} onClose={() => setDialog(undefined)} />}
      {dialog === "import" && (
        <ImportDialog parent={page.home ? { id: page.id, title: space.name } : page} spaceKey={space.key} onClose={() => setDialog(undefined)} />
      )}
      {dialog === "new" && <NewPageDialog parent={page} onClose={() => setDialog(undefined)} onDone={(made) => open(made, true)} />}
      {(dialog === "move" || dialog === "copy") && (
        <PlaceDialog
          page={{ id: page.id, title: page.title, spaceKey: space.key }}
          mode={dialog}
          onClose={() => setDialog(undefined)}
          onDone={(placed) => open(placed)}
        />
      )}
    </article>
  );
}
