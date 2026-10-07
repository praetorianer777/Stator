import { useEffect, useState, type FormEvent, type ReactNode } from "react";
import { linkedPdfHref, publicPdfHref } from "@/api/pdf";
import { PdfExportDialog } from "@/features/print/PdfExport";
import { Link, useNavigate, useRouterState } from "@tanstack/react-router";
import { useQuery } from "@tanstack/react-query";
import {
  isNotPublic,
  linkedAttachmentUrl,
  publicAttachmentUrl,
  publicPageQuery,
  usePublicPage,
  usePublicSearch,
  usePublicSite,
  usePublicSpace,
  useLinkedPage,
  type LinkedPage,
  type PublicPage,
  type PublicTreePage,
} from "@/api/public";
import { Breadcrumbs, Button, ButtonLink, EmptyState, ErrorBanner, IconButton, Input, PageHeader, Skeleton, cx } from "@/components/ui";
import { Icon } from "@/components/icons";
import { APP_NAME, PUBLIC_PATH } from "@/config";
import { DocView } from "@/features/editor/DocView";
import { pageSheet } from "@/features/pages/pageSheet";
import { PublicLinkContext, PublicReadingContext, publicPagePath, publicSitePath, publicSpacePath, signInPath } from "@/features/editor/publicReading";
import type { DocNode } from "@/features/editor/schema";
import { coverPosition } from "@/features/pages/AppearanceDialog";
import { t } from "@/i18n";
import { localDateFormat } from "@/lib/format";
import { pageSlug } from "@/lib/slug";
import { Highlight } from "@/features/search/Highlight";

const updatedAt = localDateFormat({ dateStyle: "medium" });

const PUBLIC_PAGE = new RegExp(`^${PUBLIC_PATH}/[^/]+/(?:s/[^/]+/)?p/([0-9a-f-]{36})`);
const PUBLIC_SPACE = new RegExp(`^${PUBLIC_PATH}/[^/]+/s/([A-Za-z0-9]+)/?$`);

/** The address inside the app of what a public address shows, which signing in leads back to. */
export function privateAddress(pathname: string, search: string, page?: PublicPage): string {
  const pageMatch = PUBLIC_PAGE.exec(pathname);
  if (pageMatch) {
    if (!page || page.id !== pageMatch[1]) return "/";
    return page.home ? `/s/${page.space.key}` : `/s/${page.space.key}/p/${page.id}/${pageSlug(page.title)}`;
  }
  const spaceMatch = PUBLIC_SPACE.exec(pathname);
  if (spaceMatch?.[1]) return `/s/${spaceMatch[1].toUpperCase()}`;
  if (pathname.endsWith("/search")) return `/search${search}`;
  return "/";
}

/** Search engines are asked to stay away unless the organization lets them in. */
function useRobots(indexable: boolean | undefined) {
  useEffect(() => {
    if (indexable !== false) return;
    const meta = document.createElement("meta");
    meta.name = "robots";
    meta.content = "noindex, nofollow";
    document.head.appendChild(meta);
    return () => meta.remove();
  }, [indexable]);
}

/**
 * The frame of every public page: the organization's name, a search and the
 * way to sign in, and nothing that leads into the app without it.
 */
export function PublicShell({ org, children }: { org: string; children: ReactNode }) {
  const site = usePublicSite(org);
  const navigate = useNavigate();
  const location = useRouterState({ select: (state) => state.location });
  const pageId = PUBLIC_PAGE.exec(location.pathname)?.[1];
  const page = useQuery({ ...publicPageQuery(org, pageId ?? ""), enabled: Boolean(pageId) });
  const [q, setQ] = useState("");
  useRobots(site.data?.site.indexable);

  function search(event: FormEvent) {
    event.preventDefault();
    if (q.trim()) void navigate({ to: "/public/$org/search", params: { org }, search: { q: q.trim() } });
  }

  const next = privateAddress(location.pathname, location.searchStr, page.data);
  return (
    <PublicReadingContext value={org}>
      <div className="min-h-full bg-backdrop" data-public-site={org}>
        <header className="border-b border-border bg-surface">
          <div className="mx-auto flex max-w-6xl flex-wrap items-center gap-3 px-4 py-3">
            <Link to="/public/$org" params={{ org }} className="mr-auto font-semibold text-ink no-underline" data-public-home="">
              {site.data?.site.name ?? APP_NAME}
            </Link>
            {site.data && (
              <search className="flex min-w-0 items-center gap-2">
                <form onSubmit={search} className="flex items-center gap-1">
                  <Input
                    type="search"
                    value={q}
                    onChange={(event) => setQ(event.target.value)}
                    placeholder={t.publicReading.searchPlaceholder}
                    aria-label={t.publicReading.searchLabel}
                    controlSize="sm"
                    className="w-40 sm:w-56"
                    data-public-search=""
                  />
                  <IconButton type="submit" icon={<Icon.Search />} label={t.publicReading.search} size="sm" />
                </form>
              </search>
            )}
            <ButtonLink href={signInPath(next, org)} size="sm" variant="primary" data-action="public-sign-in">
              {t.publicReading.signIn}
            </ButtonLink>
          </div>
        </header>
        <main className="mx-auto max-w-6xl px-4 py-6">
          {site.error ? <Failed error={site.error} org={org} next={next} retry={() => void site.refetch()} /> : site.data ? children : <Skeleton />}
        </main>
      </div>
    </PublicReadingContext>
  );
}

/** What somebody who is not signed in is told in place of what is not public. */
export function NotPublic({ org, next }: { org: string; next: string }) {
  return (
    <div data-not-public="">
      <EmptyState
        icon={<Icon.Lock />}
        title={t.publicReading.notPublicTitle}
        description={t.publicReading.notPublicBody}
        action={
          <ButtonLink href={signInPath(next, org)} variant="primary">
            {t.publicReading.signInToRead}
          </ButtonLink>
        }
      />
    </div>
  );
}

function Failed({ error, org, next, retry }: { error: unknown; org: string; next: string; retry: () => void }) {
  if (isNotPublic(error)) return <NotPublic org={org} next={next} />;
  return <ErrorBanner onRetry={retry}>{t.publicReading.failed}</ErrorBanner>;
}

/**
 * The one page a public link opens: the organization's name, the page and
 * the way to sign in, and no tree, search or other page of the app.
 */
export function PublicLinkScreen({ org, token }: { org: string; token: string }) {
  const linked = useLinkedPage(org, token);
  useRobots(linked.error ? false : linked.data?.site.indexable);
  return (
    <PublicReadingContext value={org}>
      <PublicLinkContext value={token}>
        <div className="min-h-full bg-backdrop" data-public-link={org}>
          <header className="border-b border-border bg-surface">
            <div className="mx-auto flex max-w-6xl flex-wrap items-center gap-3 px-4 py-3">
              <span className="mr-auto font-semibold text-ink">{linked.data?.site.name ?? APP_NAME}</span>
              <ButtonLink href={signInPath("/", org)} size="sm" variant="primary" data-action="public-sign-in">
                {t.publicReading.signIn}
              </ButtonLink>
            </div>
          </header>
          <main className="mx-auto max-w-6xl px-4 py-6">
            {linked.error ? (
              isNotPublic(linked.error) ? (
                <div data-link-gone="">
                  <EmptyState icon={<Icon.Lock />} title={t.publicLinks.goneTitle} description={t.publicLinks.goneBody} />
                </div>
              ) : (
                <ErrorBanner onRetry={() => void linked.refetch()}>{t.publicReading.failed}</ErrorBanner>
              )
            ) : linked.data ? (
              <LinkedArticle org={org} token={token} page={linked.data.page} />
            ) : (
              <Skeleton />
            )}
          </main>
        </div>
      </PublicLinkContext>
    </PublicReadingContext>
  );
}

/** Prints the page shown as PDF, from a button beside its title. */
function PdfButton({ href, title }: { href: string; title: string }) {
  const [open, setOpen] = useState(false);
  return (
    <>
      <Button variant="secondary" icon={<Icon.File />} onClick={() => setOpen(true)} aria-label={t.pdf.exportLabel} data-action="export-pdf">
        {t.pdf.exportButton}
      </Button>
      {open && <PdfExportDialog href={href} title={title} onClose={() => setOpen(false)} />}
    </>
  );
}

function LinkedArticle({ org, token, page }: { org: string; token: string; page: LinkedPage }) {
  return (
    <article {...pageSheet(page.appearance.width)} data-public-page={page.id}>
      {page.appearance.cover && (
        <div className="page-cover">
          <img
            src={linkedAttachmentUrl(org, token, page.appearance.cover.attachmentId, true)}
            alt=""
            style={{ objectPosition: coverPosition(page.appearance.cover) }}
          />
        </div>
      )}
      <PageHeader
        title={
          <>
            {page.appearance.icon && <span className="mr-2">{page.appearance.icon}</span>}
            <span data-page-title>{page.title}</span>
          </>
        }
        meta={<span>{t.publicReading.updated(updatedAt.format(new Date(page.updatedAt)))}</span>}
        actions={page.kind !== "folder" && <PdfButton href={linkedPdfHref(org, token)} title={page.title} />}
      />
      <DocView doc={page.body as DocNode} />
    </article>
  );
}

/** The organization's public spaces. */
export function PublicSiteScreen({ org }: { org: string }) {
  const site = usePublicSite(org);
  if (!site.data) return null;
  return (
    <div className="mx-auto max-w-3xl" data-public-spaces="">
      <PageHeader title={site.data.site.name} />
      <h2 className="mb-3 text-sm font-semibold text-ink-muted">{t.publicReading.spaces}</h2>
      {site.data.spaces.length === 0 ? (
        <EmptyState title={t.publicReading.noSpaces} />
      ) : (
        <ul className="space-y-2">
          {site.data.spaces.map((space) => (
            <li key={space.key} className="rounded-control border border-border bg-surface p-3" data-public-space={space.key}>
              <Link to="/public/$org/s/$spaceKey" params={{ org, spaceKey: space.key }} className="font-medium">
                {space.name}
              </Link>
              {space.description && <p className="mt-1 text-sm text-ink-muted">{space.description}</p>}
            </li>
          ))}
        </ul>
      )}
    </div>
  );
}

/** A public space: its home page beside its tree. */
export function PublicSpaceScreen({ org, spaceKey }: { org: string; spaceKey: string }) {
  const space = usePublicSpace(org, spaceKey);
  if (space.error) return <Failed error={space.error} org={org} next={`/s/${spaceKey.toUpperCase()}`} retry={() => void space.refetch()} />;
  if (!space.data) return <Skeleton />;
  return (
    <WithTree org={org} pages={space.data.pages} current={space.data.space.homePageId}>
      <PageArticle org={org} pageId={space.data.space.homePageId} />
    </WithTree>
  );
}

/** A public page beside its space's tree. */
export function PublicPageScreen({ org, pageId }: { org: string; pageId: string }) {
  const page = usePublicPage(org, pageId);
  const space = usePublicSpace(org, page.data?.space.key ?? "");
  if (page.error) return <Failed error={page.error} org={org} next="/" retry={() => void page.refetch()} />;
  if (!page.data) return <Skeleton />;
  return (
    <WithTree org={org} pages={space.data?.pages ?? []} current={pageId}>
      <PageArticle org={org} pageId={pageId} />
    </WithTree>
  );
}

function WithTree({ org, pages, current, children }: { org: string; pages: PublicTreePage[]; current: string; children: ReactNode }) {
  return (
    <div className="flex flex-col gap-6 md:flex-row">
      {pages.length > 0 && (
        <nav aria-label={t.publicReading.pages} className="shrink-0 md:w-60" data-public-tree="">
          <ul className="space-y-0.5 text-sm">
            {pages.map((page) => (
              <li key={page.id} style={{ paddingLeft: `${page.depth * 0.75}rem` }}>
                <TreeLink org={org} page={page} current={page.id === current} />
              </li>
            ))}
          </ul>
        </nav>
      )}
      <div className="min-w-0 flex-1">{children}</div>
    </div>
  );
}

function TreeLink({ org, page, current }: { org: string; page: PublicTreePage; current: boolean }) {
  const className = cx(
    "block truncate rounded-control px-2 py-1 no-underline",
    current ? "bg-surface-raised font-medium text-ink" : "text-ink-muted hover:text-ink",
  );
  const label = (
    <>
      {page.icon && <span className="mr-1.5">{page.icon}</span>}
      {page.title}
    </>
  );
  return (
    <a href={pageHref(org, page)} className={className} aria-current={current ? "page" : undefined} data-public-tree-page={page.title}>
      {label}
    </a>
  );
}

function pageHref(org: string, page: { id: string; title: string }, spaceKey?: string): string {
  return publicPagePath(org, page.id, spaceKey, pageSlug(page.title));
}

function PageArticle({ org, pageId }: { org: string; pageId: string }) {
  const { data: page } = usePublicPage(org, pageId);
  if (!page) return <Skeleton />;
  const crumbs = [
    { label: page.space.name, render: (label: ReactNode) => <a href={publicSpacePath(org, page.space.key)}>{label}</a> },
    ...page.ancestors
      .filter((ancestor) => ancestor.id !== page.space.homePageId)
      .map((ancestor) => ({ label: ancestor.title, render: (label: ReactNode) => <a href={pageHref(org, ancestor, page.space.key)}>{label}</a> })),
  ];
  return (
    <article {...pageSheet(page.appearance.width)} data-public-page={page.id}>
      {page.appearance.cover && (
        <div className="page-cover">
          <img
            src={publicAttachmentUrl(org, page.appearance.cover.attachmentId, true)}
            alt=""
            style={{ objectPosition: coverPosition(page.appearance.cover) }}
          />
        </div>
      )}
      <PageHeader
        crumbs={page.home ? [] : crumbs}
        title={
          <>
            {page.appearance.icon && <span className="mr-2">{page.appearance.icon}</span>}
            <span data-page-title>{page.title}</span>
          </>
        }
        meta={<span>{t.publicReading.updated(updatedAt.format(new Date(page.updatedAt)))}</span>}
        actions={page.kind !== "folder" && <PdfButton href={publicPdfHref(org, page.id)} title={page.title} />}
      />
      {page.kind === "folder" ? <FolderChildren org={org} page={page} /> : <DocView doc={page.body as DocNode} />}
    </article>
  );
}

function FolderChildren({ org, page }: { org: string; page: PublicPage }) {
  if (page.children.length === 0) return <p className="text-sm text-ink-muted">{t.publicReading.noChildren}</p>;
  return (
    <nav aria-label={t.publicReading.children}>
      <ul className="list-disc space-y-1 pl-5">
        {page.children.map((child) => (
          <li key={child.id}>
            <a href={pageHref(org, child, page.space.key)}>{child.title}</a>
          </li>
        ))}
      </ul>
    </nav>
  );
}

/** A page by its id alone, as an included page links to it: once found, its full address. */
export function PublicPageLookup({ org, pageId }: { org: string; pageId: string }) {
  const page = usePublicPage(org, pageId);
  const navigate = useNavigate();
  useEffect(() => {
    if (!page.data) return;
    const { space, title, home } = page.data;
    if (home) void navigate({ to: "/public/$org/s/$spaceKey", params: { org, spaceKey: space.key }, replace: true });
    else void navigate({ to: "/public/$org/s/$spaceKey/p/$pageId/$slug", params: { org, spaceKey: space.key, pageId, slug: pageSlug(title) }, replace: true });
  }, [page.data, navigate, org, pageId]);
  if (page.error) return <Failed error={page.error} org={org} next="/" retry={() => void page.refetch()} />;
  return <Skeleton />;
}

/** The public pages whose words match, a window at a time. */
export function PublicSearchScreen({ org, q, page }: { org: string; q: string; page: number }) {
  const found = usePublicSearch(org, q, page);
  return (
    <div className="mx-auto max-w-3xl" data-public-results="">
      <Breadcrumbs crumbs={[{ label: t.publicReading.allSpaces, render: (label) => <a href={publicSitePath(org)}>{label}</a> }]} className="mb-2" />
      <PageHeader title={t.publicReading.searchTitle(q)} />
      {found.error && <ErrorBanner onRetry={() => void found.refetch()}>{found.error.message}</ErrorBanner>}
      {!found.data && !found.error && q.trim() !== "" && <Skeleton />}
      {found.data && found.data.hits.length === 0 && <EmptyState title={t.publicReading.noHits} />}
      <ul className="space-y-4">
        {found.data?.hits.map((hit) => (
          <li key={hit.page.id} data-public-hit={hit.page.title}>
            <a href={pageHref(org, hit.page, hit.page.spaceKey)} className="font-medium">
              <Highlight segments={hit.title} />
            </a>
            <p className="text-xs text-ink-subtle">{hit.page.spaceName}</p>
            <p className="mt-1 text-sm text-ink-muted">
              <Highlight segments={hit.snippet} />
            </p>
          </li>
        ))}
      </ul>
      <div className="mt-6 flex gap-3">
        {page > 1 && (
          <Link to="/public/$org/search" params={{ org }} search={{ q, page: page - 1 }}>
            {t.publicReading.earlier}
          </Link>
        )}
        {found.data?.more && (
          <Link to="/public/$org/search" params={{ org }} search={{ q, page: page + 1 }}>
            {t.publicReading.more}
          </Link>
        )}
      </div>
    </div>
  );
}
