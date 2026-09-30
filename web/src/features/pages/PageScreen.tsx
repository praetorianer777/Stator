import { useState } from "react";
import { Link, useNavigate } from "@tanstack/react-router";
import { usePage, type Page } from "@/api/pages";
import type { Space } from "@/api/spaces";
import { useTrashPage } from "@/api/trash";
import { Button, ErrorBanner, IconButton, Menu, PageHeader, Skeleton, Tag, Tooltip, type Crumb, type MenuItem } from "@/components/ui";
import { Icon } from "@/components/icons";
import { DocView } from "@/features/editor/DocView";
import { RestrictionsDialog } from "@/features/permissions/RestrictionsDialog";
import { t } from "@/i18n";
import { pageSlug } from "@/lib/slug";
import { NewPageDialog } from "./NewPageDialog";
import { PageLink } from "./PageLink";
import { PlaceDialog } from "./PlaceDialog";

const updatedAt = new Intl.DateTimeFormat(undefined, { dateStyle: "medium" });

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

type Dialog = "new" | "move" | "copy" | "restrictions";

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

/** A page as a reader sees it: its place, its title, who last changed it, and its document. */
export function PageScreen({ pageId }: { pageId: string }) {
  const { data, isLoading, error, refetch } = usePage(pageId);
  const navigate = useNavigate();
  const [dialog, setDialog] = useState<Dialog>();
  // A dialog is about the page it was opened on, so going to another page closes it.
  const [dialogPage, setDialogPage] = useState(pageId);
  if (dialogPage !== pageId) {
    setDialogPage(pageId);
    setDialog(undefined);
  }
  const trash = useTrashPage(data?.space.key ?? "");
  if (error) return <ErrorBanner onRetry={() => void refetch()}>{error.message}</ErrorBanner>;
  if (isLoading || !data) return <Skeleton />;
  const { page, space } = data;
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
  if (page.can.restrict) {
    actions.push({ label: t.restrictions.menu, icon: <Icon.Lock />, onSelect: () => setDialog("restrictions"), attrs: { "data-action": "page-restrictions" } });
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
          </>
        }
        meta={
          <span className="flex flex-wrap items-center gap-2">
            {t.page.updated(page.updatedByName, updatedAt.format(new Date(page.updatedAt)))}
            {(page.restricted.view || page.restricted.edit) && <RestrictedBadge page={page} onOpen={() => setDialog("restrictions")} />}
          </span>
        }
        actions={
          <>
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
      <DocView doc={page.body} />
      {dialog === "restrictions" && <RestrictionsDialog page={page} spaceKey={space.key} onClose={() => setDialog(undefined)} />}
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
