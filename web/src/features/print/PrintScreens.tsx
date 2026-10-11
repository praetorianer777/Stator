import { useEffect, type CSSProperties, type ReactNode } from "react";
import { useMe } from "@/api/auth";
import { attachmentUrl } from "@/api/attachments";
import { usePage } from "@/api/pages";
import { linkedAttachmentUrl, publicAttachmentUrl, useLinkedPage, usePublicPage, usePublicSite } from "@/api/public";
import { useDefaultTheme, type Theme } from "@/api/themes";
import { usePageAttachmentIds } from "@/features/attachments/hooks";
import { footerLine, publicLogoHref, useBrandPicture, useOrgPrintBrand } from "./brandMeta";
import { KnownAttachmentsContext } from "@/features/editor/attachmentIndex";
import { DocPageContext } from "@/features/editor/BlockViews";
import { DocView } from "@/features/editor/DocView";
import { PublicLinkContext, PublicReadingContext } from "@/features/editor/publicReading";
import type { DocNode } from "@/features/editor/schema";
import { coverPosition } from "@/features/pages/AppearanceDialog";
import { pageSheet } from "@/features/pages/pageSheet";
import { t } from "@/i18n";
import { localDateFormat } from "@/lib/format";
import { applyCustomTheme } from "@/lib/theme";
import { compileTheme } from "@/lib/theme-css";
import { usePrintReady } from "./printReady";

// The views the render service prints a page from: the page alone, with no
// shell, menus or comments, in the light scheme on paper. The running header
// and footer are drawn by the browser from the meta each view states.

const day = localDateFormat({ dateStyle: "medium" });

/** What the running header and footer of every sheet say. */
interface PrintMeta {
  header: string;
  title: string;
  footer: string;
  /** The organization's logo as a data address, and its footer line; either may be empty. */
  logo?: string;
  brandFooter?: string;
}

/** What a printed page shows above its words. */
interface Printed {
  title: string;
  icon?: string | null;
  cover?: { url: string; style: CSSProperties } | null;
  byline: string;
}

/** Paper is light, so a print takes the light scheme whatever the browser prefers. */
function useLightScheme(): void {
  useEffect(() => {
    const html = document.documentElement;
    const before = html.getAttribute("data-theme");
    html.setAttribute("data-theme", "light");
    return () => {
      if (before === null) html.removeAttribute("data-theme");
      else html.setAttribute("data-theme", before);
    };
  }, []);
}

/** Draws in a theme of the organization's, or the built-in one for null. */
function useTheme(theme: Theme | null | undefined): void {
  useEffect(() => {
    if (theme === undefined) return;
    applyCustomTheme(theme ? compileTheme(theme) : null);
    return () => applyCustomTheme(null);
  }, [theme]);
}

function footer(version: number, updatedAt: string): string {
  return t.pdf.footer(version, day.format(new Date(updatedAt)), day.format(new Date()));
}

/** The frame of every print view: ready once loaded and drawn, or why it cannot be printed. */
function PrintFrame({ meta, failed, children }: { meta?: PrintMeta; failed?: string; children?: ReactNode }) {
  useLightScheme();
  usePrintReady(Boolean(meta) && !failed);
  useEffect(() => {
    if (meta) document.title = meta.title;
  }, [meta]);
  if (failed) {
    return (
      <main className="print-view">
        <p role="alert" data-print-failed="">
          {failed}
        </p>
      </main>
    );
  }
  return (
    <main className="print-view" data-print-view="">
      {meta && (
        <div
          hidden
          data-print-meta=""
          data-header={meta.header}
          data-title={meta.title}
          data-footer={meta.footer}
          data-logo={meta.logo}
          data-brand-footer={meta.brandFooter}
          data-pages={t.pdf.pages}
        />
      )}
      {children}
    </main>
  );
}

/** A page as printed: its cover, its title and where it comes from, then its words. */
function PrintArticle({ page, children }: { page: Printed; children: ReactNode }) {
  const sheet = pageSheet("full");
  return (
    <article className={sheet.className} style={sheet.style} data-print-page="">
      {page.cover && (
        <div className="page-cover">
          <img src={page.cover.url} alt="" style={page.cover.style} />
        </div>
      )}
      <h1 className="mb-1 text-2xl font-semibold text-ink">
        {page.icon && <span className="mr-2">{page.icon}</span>}
        <span data-page-title="">{page.title}</span>
      </h1>
      <p className="mb-6 text-sm text-ink-muted" data-print-byline="">
        {page.byline}
      </p>
      {children}
    </article>
  );
}

/** A published page as the signed-in reader reads it, in the organization's theme. */
export function PrintPageScreen({ pageId }: { pageId: string }) {
  const { data, error } = usePage(pageId);
  const me = useMe();
  const theme = useDefaultTheme();
  const brand = useOrgPrintBrand();
  const attachmentIds = usePageAttachmentIds(pageId);
  useTheme(theme.isPending ? undefined : (theme.data ?? null));
  if (error) return <PrintFrame failed={error.message} />;
  if (!data || !me.data || !brand) return <PrintFrame />;
  const { page, space } = data;
  const org = me.data.organization?.name ?? "";
  const meta = {
    header: t.pdf.header([org, space.name]),
    title: page.title,
    footer: footer(page.version, page.updatedAt),
    logo: brand.logo,
    brandFooter: brand.footer,
  };
  const cover = page.appearance.cover;
  return (
    <PrintFrame meta={meta}>
      <PrintArticle
        page={{
          title: page.title,
          icon: page.appearance.icon,
          cover: cover && { url: attachmentUrl(cover.attachmentId, true), style: { objectPosition: coverPosition(cover) } },
          byline: t.pdf.header([space.name, meta.footer]),
        }}
      >
        <KnownAttachmentsContext value={attachmentIds}>
          <DocPageContext value={{ id: page.id, spaceKey: space.key }}>
            <DocView doc={page.body} />
          </DocPageContext>
        </KnownAttachmentsContext>
      </PrintArticle>
    </PrintFrame>
  );
}

/** A page anybody may read, as anybody reads it. */
export function PrintPublicPageScreen({ org, pageId }: { org: string; pageId: string }) {
  const page = usePublicPage(org, pageId);
  const site = usePublicSite(org);
  const brand = useBrandPicture(
    publicLogoHref(org, site.data?.site.logoVersion ?? null),
    footerLine(site.data?.site.footer ?? { en: "", de: "" }),
    Boolean(site.data),
  );
  const failed = page.error ?? site.error;
  if (failed) return <PrintFrame failed={failed.message} />;
  if (!page.data || !site.data || !brand) return <PrintFrame />;
  const p = page.data;
  const meta = {
    header: t.pdf.header([site.data.site.name, p.space.name]),
    title: p.title,
    footer: footer(p.version, p.updatedAt),
    logo: brand.logo,
    brandFooter: brand.footer,
  };
  const cover = p.appearance.cover;
  return (
    <PrintFrame meta={meta}>
      <PublicReadingContext value={org}>
        <PrintArticle
          page={{
            title: p.title,
            icon: p.appearance.icon,
            cover: cover && { url: publicAttachmentUrl(org, cover.attachmentId, true), style: { objectPosition: coverPosition(cover) } },
            byline: t.pdf.header([p.space.name, meta.footer]),
          }}
        >
          <DocView doc={p.body as DocNode} />
        </PrintArticle>
      </PublicReadingContext>
    </PrintFrame>
  );
}

/** The page a public link opens, as the link shows it: nothing of its space. */
export function PrintLinkedPageScreen({ org, token }: { org: string; token: string }) {
  const linked = useLinkedPage(org, token);
  const site = linked.data?.site;
  const brand = useBrandPicture(publicLogoHref(org, site?.logoVersion ?? null, token), footerLine(site?.footer ?? { en: "", de: "" }), Boolean(site));
  if (linked.error) return <PrintFrame failed={linked.error.message} />;
  if (!linked.data || !site || !brand) return <PrintFrame />;
  const { page } = linked.data;
  const meta = { header: site.name, title: page.title, footer: footer(page.version, page.updatedAt), logo: brand.logo, brandFooter: brand.footer };
  const cover = page.appearance.cover;
  return (
    <PrintFrame meta={meta}>
      <PublicReadingContext value={org}>
        <PublicLinkContext value={token}>
          <PrintArticle
            page={{
              title: page.title,
              icon: page.appearance.icon,
              cover: cover && { url: linkedAttachmentUrl(org, token, cover.attachmentId, true), style: { objectPosition: coverPosition(cover) } },
              byline: meta.footer,
            }}
          >
            <DocView doc={page.body as DocNode} />
          </PrintArticle>
        </PublicLinkContext>
      </PublicReadingContext>
    </PrintFrame>
  );
}
