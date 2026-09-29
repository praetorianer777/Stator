import { Link, useNavigate } from "@tanstack/react-router";
import { usePage, type Page } from "@/api/pages";
import type { Space } from "@/api/spaces";
import { Button, ErrorBanner, PageHeader, Skeleton, type Crumb } from "@/components/ui";
import { Icon } from "@/components/icons";
import { DocView } from "@/features/editor/DocView";
import { t } from "@/i18n";
import { pageSlug } from "@/lib/slug";

const updatedAt = new Intl.DateTimeFormat(undefined, { dateStyle: "medium" });

/** The trail above a page's title: the directory, its space, then whatever is above it. */
export function pageCrumbs(space: Space, page: Page): Crumb[] {
  const crumbs: Crumb[] = [{ label: t.spaces.title, render: (label) => <Link to="/spaces">{label}</Link> }];
  if (!page.home) {
    crumbs.push({
      label: space.name,
      render: (label) => (
        <Link to="/s/$spaceKey" params={{ spaceKey: space.key }}>
          {label}
        </Link>
      ),
    });
  }
  return crumbs;
}

/** A page as a reader sees it: its place, its title, who last changed it, and its document. */
export function PageScreen({ pageId }: { pageId: string }) {
  const { data, isLoading, error, refetch } = usePage(pageId);
  const navigate = useNavigate();
  if (error) return <ErrorBanner onRetry={() => void refetch()}>{error.message}</ErrorBanner>;
  if (isLoading || !data) return <Skeleton />;
  const { page, space } = data;
  const edit = () => navigate({ to: "/s/$spaceKey/p/$pageId/$slug/edit", params: { spaceKey: space.key, pageId: page.id, slug: pageSlug(page.title) } });

  return (
    <article className="mx-auto max-w-3xl" data-page={page.id} data-page-home={page.home || undefined}>
      <PageHeader
        crumbs={pageCrumbs(space, page)}
        title={<span data-page-title>{page.title}</span>}
        meta={t.page.updated(page.updatedByName, updatedAt.format(new Date(page.updatedAt)))}
        actions={
          space.can.editPages && (
            <Button variant="secondary" icon={<Icon.Edit />} onClick={edit} data-action="edit-page">
              {t.page.edit}
            </Button>
          )
        }
      />
      <DocView doc={page.body} />
    </article>
  );
}
