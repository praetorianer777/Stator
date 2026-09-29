import { useState } from "react";
import { Link, useNavigate } from "@tanstack/react-router";
import { usePage, type Page } from "@/api/pages";
import type { Space } from "@/api/spaces";
import { Button, ErrorBanner, IconButton, Menu, PageHeader, Skeleton, type Crumb, type MenuItem } from "@/components/ui";
import { Icon } from "@/components/icons";
import { DocView } from "@/features/editor/DocView";
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

type Dialog = "new" | "move" | "copy";

/** A page as a reader sees it: its place, its title, who last changed it, and its document. */
export function PageScreen({ pageId }: { pageId: string }) {
  const { data, isLoading, error, refetch } = usePage(pageId);
  const navigate = useNavigate();
  const [dialog, setDialog] = useState<Dialog>();
  if (error) return <ErrorBanner onRetry={() => void refetch()}>{error.message}</ErrorBanner>;
  if (isLoading || !data) return <Skeleton />;
  const { page, space } = data;
  const edit = () => navigate({ to: "/s/$spaceKey/p/$pageId/$slug/edit", params: { spaceKey: space.key, pageId: page.id, slug: pageSlug(page.title) } });
  const open = (placed: Page, editing = false) => {
    setDialog(undefined);
    const params = { spaceKey: placed.spaceKey, pageId: placed.id, slug: pageSlug(placed.title) };
    void navigate(editing ? { to: "/s/$spaceKey/p/$pageId/$slug/edit", params } : { to: "/s/$spaceKey/p/$pageId/$slug", params });
  };

  const actions: MenuItem[] = [];
  if (!page.home) actions.push({ label: t.page.move, onSelect: () => setDialog("move"), attrs: { "data-action": "move-page" } });
  actions.push({ label: t.page.copy, onSelect: () => setDialog("copy"), attrs: { "data-action": "copy-page" } });

  return (
    <article className="mx-auto max-w-3xl" data-page={page.id} data-page-home={page.home || undefined}>
      <PageHeader
        crumbs={pageCrumbs(space, page)}
        title={<span data-page-title>{page.title}</span>}
        meta={t.page.updated(page.updatedByName, updatedAt.format(new Date(page.updatedAt)))}
        actions={
          space.can.editPages && (
            <>
              <Button variant="secondary" icon={<Icon.Plus />} onClick={() => setDialog("new")} data-action="new-page">
                {t.page.newPage}
              </Button>
              <Button variant="secondary" icon={<Icon.Edit />} onClick={edit} data-action="edit-page">
                {t.page.edit}
              </Button>
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
            </>
          )
        }
      />
      <DocView doc={page.body} />
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
