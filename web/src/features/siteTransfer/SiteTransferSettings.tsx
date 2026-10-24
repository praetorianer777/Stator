import { useRef, useState, type FormEvent } from "react";
import { formatSize } from "@/api/attachments";
import { ApiError } from "@/api/client";
import {
  siteExportHref,
  useCreateSiteExport,
  useImportSite,
  useSiteExports,
  useSiteImports,
  type SiteExport,
  type SiteImport,
  type SiteImportReport,
} from "@/api/siteTransfers";
import { transferOpen } from "@/api/spaceTransfers";
import { Button, ButtonLink, Card, ErrorBanner, PageHeader, SectionTitle, Skeleton } from "@/components/ui";
import { Icon } from "@/components/icons";
import { language, t } from "@/i18n";
import { settingsCrumb } from "@/features/settings/SettingsHome";

/** Where an administrator exports the whole organization as one archive, and fills a fresh one from such an archive. */
export function SiteTransferSettings() {
  return (
    <div className="mx-auto max-w-3xl space-y-8" data-site-transfer="">
      <PageHeader crumbs={[settingsCrumb()]} title={t.siteTransfer.title} />
      <SiteExportSection />
      <SiteImportSection />
      <NotCarried />
    </div>
  );
}

function dateTime() {
  return new Intl.DateTimeFormat(language(), { dateStyle: "medium", timeStyle: "short" });
}

function percentOf(progress: { done: number; total: number }): number {
  return progress.total > 0 ? Math.round((progress.done / progress.total) * 100) : 0;
}

function SiteExportSection() {
  const exports = useSiteExports();
  const create = useCreateSiteExport();
  const underWay = create.isPending || (exports.data?.some(transferOpen) ?? false);
  return (
    <section aria-labelledby="site-export-title" className="space-y-4" data-site-export="">
      <SectionTitle id="site-export-title">{t.siteTransfer.exportTitle}</SectionTitle>
      <p className="text-sm text-ink-muted">{t.siteTransfer.exportIntro}</p>
      {create.error && <ErrorBanner>{create.error.message}</ErrorBanner>}
      <Button icon={<Icon.Download />} loading={create.isPending} disabled={underWay} onClick={() => create.mutate()} data-action="export-site">
        {underWay ? t.siteTransfer.exporting : t.siteTransfer.export}
      </Button>
      {exports.isLoading && <Skeleton lines={2} />}
      {exports.error && <ErrorBanner onRetry={() => void exports.refetch()}>{exports.error.message}</ErrorBanner>}
      {exports.data && exports.data.length === 0 && <p className="text-sm text-ink-muted">{t.siteTransfer.noExports}</p>}
      <ul className="space-y-2">
        {exports.data?.map((job) => (
          <ExportRow key={job.id} job={job} />
        ))}
      </ul>
    </section>
  );
}

function ExportRow({ job }: { job: SiteExport }) {
  const when = dateTime();
  const percent = percentOf(job.progress);
  return (
    <li data-site-export-row={job.state} data-export-id={job.id}>
      <Card className="flex flex-wrap items-center justify-between gap-3 p-3">
        <div className="min-w-0 space-y-1">
          <p className="text-sm font-medium text-ink">{t.siteTransfer.archive}</p>
          <p className="text-xs text-ink-muted">{t.spaceTransfer.askedBy(job.requestedBy, when.format(new Date(job.requestedAt)))}</p>
          {transferOpen(job) && (
            <>
              <p className="text-xs text-ink-muted" role="status">
                {job.state === "queued" ? t.spaceTransfer.queued : t.spaceTransfer.progress(job.progress.done, job.progress.total)}
              </p>
              <progress className="h-1.5 w-48 max-w-full accent-accent" max={100} value={percent} aria-label={t.spaceTransfer.progressLabel(percent)} />
            </>
          )}
          {job.state === "done" && job.expiresAt && (
            <p className="text-xs text-ink-muted">{t.spaceTransfer.expires(when.format(new Date(job.expiresAt)), formatSize(job.size ?? 0))}</p>
          )}
          {job.state === "expired" && <p className="text-xs text-ink-muted">{t.spaceTransfer.expired}</p>}
          {job.state === "failed" && job.message && <p className="text-xs text-danger">{job.message}</p>}
        </div>
        {job.state === "done" && (
          <ButtonLink
            href={siteExportHref(job.id)}
            variant="secondary"
            icon={<Icon.Download />}
            download={job.fileName ?? undefined}
            data-action="download-site-export"
          >
            {t.spaceTransfer.download}
          </ButtonLink>
        )}
      </Card>
    </li>
  );
}

function SiteImportSection() {
  const [file, setFile] = useState<File | null>(null);
  const input = useRef<HTMLInputElement>(null);
  const upload = useImportSite();
  const imports = useSiteImports();
  const fields = upload.error instanceof ApiError ? upload.error.fields : {};
  const latest = imports.data?.[0];

  function submit(event: FormEvent) {
    event.preventDefault();
    if (file) upload.mutate(file, { onSuccess: () => setFile(null) });
  }

  const error = upload.error && !fields.file ? upload.error.message : undefined;
  return (
    <section aria-labelledby="site-import-title" className="space-y-4" data-site-import="">
      <SectionTitle id="site-import-title">{t.siteTransfer.importTitle}</SectionTitle>
      <p className="text-sm text-ink-muted">{t.siteTransfer.importIntro}</p>
      <form onSubmit={submit} className="space-y-4" noValidate>
        {error && <ErrorBanner>{error}</ErrorBanner>}
        <input
          ref={input}
          type="file"
          accept=".zip,application/zip"
          className="sr-only"
          tabIndex={-1}
          aria-hidden="true"
          onChange={(event) => setFile(event.target.files?.[0] ?? null)}
          data-site-import-file=""
        />
        <div className="space-y-1">
          <Button type="button" variant="secondary" icon={<Icon.File />} onClick={() => input.current?.click()} data-action="choose-site-archive">
            {t.spaceTransfer.chooseArchive}
          </Button>
          <p className="text-sm text-ink" aria-live="polite" data-chosen-site-archive={file?.name ?? ""}>
            {file ? file.name : <span className="text-ink-muted">{t.spaceTransfer.noArchive}</span>}
          </p>
          {fields.file && <p className="text-sm text-danger">{fields.file}</p>}
        </div>
        {upload.isPending && (
          <progress
            className="h-1.5 w-full accent-accent"
            max={100}
            value={Math.round(upload.progress * 100)}
            aria-label={t.spaceTransfer.sending(Math.round(upload.progress * 100))}
          />
        )}
        <Button type="submit" icon={<Icon.Upload />} loading={upload.isPending} disabled={!file} data-action="confirm-import-site">
          {t.siteTransfer.importSite}
        </Button>
      </form>
      {imports.error && <ErrorBanner onRetry={() => void imports.refetch()}>{imports.error.message}</ErrorBanner>}
      {latest && <ImportState job={latest} />}
    </section>
  );
}

function ImportState({ job }: { job: SiteImport }) {
  const percent = percentOf(job.progress);
  return (
    <div className="space-y-3" data-site-import-state={job.state}>
      <p className="text-xs text-ink-muted">{t.siteTransfer.importedBy(job.requestedBy, dateTime().format(new Date(job.requestedAt)))}</p>
      {transferOpen(job) && (
        <div className="space-y-2">
          <p className="text-sm text-ink" role="status">
            {job.state === "running" ? t.spaceTransfer.progress(job.progress.done, job.progress.total) : t.spaceTransfer.importQueued}
          </p>
          <progress className="h-1.5 w-full accent-accent" max={100} value={percent} aria-label={t.spaceTransfer.progressLabel(percent)} />
        </div>
      )}
      {job.state === "failed" && job.message && <ErrorBanner>{job.message}</ErrorBanner>}
      {job.state === "done" && (
        <>
          <p className="text-sm text-ink" role="status" data-site-import-done="">
            {t.siteTransfer.imported}
          </p>
          {job.report && <ImportReport report={job.report} />}
        </>
      )}
    </div>
  );
}

function ImportReport({ report }: { report: SiteImportReport }) {
  const r = t.siteTransfer.report;
  const rows: Array<[string, number]> = [
    [r.spaces, report.spaces],
    [r.pages, report.pages],
    [r.versions, report.versions],
    [r.files, report.files],
    [r.comments, report.comments],
    [r.drafts, report.drafts],
    [r.people, report.people],
    [r.matched, report.matched],
    [r.groups, report.groups],
    [r.templates, report.templates],
    [r.themes, report.themes],
    [r.webhooks, report.webhooks],
    [r.auditEntries, report.auditEntries],
  ];
  const sr = t.spaceTransfer.report;
  return (
    <Card className="space-y-4 p-4" data-site-import-report="">
      <SectionTitle>{r.title}</SectionTitle>
      <dl className="grid grid-cols-[1fr_auto] gap-x-6 gap-y-1 text-sm">
        {rows.map(([label, n]) => (
          <div key={label} className="contents">
            <dt className="text-ink-muted">{label}</dt>
            <dd className="text-right tabular-nums text-ink">{n}</dd>
          </div>
        ))}
      </dl>
      {(report.kept.length > 0 || report.webhooks > 0 || report.reattributed > 0 || report.droppedReactions > 0) && (
        <ul className="list-disc pl-5 text-sm text-ink-muted" data-site-import-notes="">
          {report.kept.map((k) => (
            <li key={k}>{r.kept[k] ?? k}</li>
          ))}
          {report.webhooks > 0 && <li>{r.webhooksOff}</li>}
          {report.reattributed > 0 && <li>{sr.reattributed(report.reattributed)}</li>}
          {report.droppedReactions > 0 && <li>{sr.droppedReactions(report.droppedReactions)}</li>}
        </ul>
      )}
      {report.dropped.length > 0 && (
        <section aria-labelledby="site-import-dropped" className="space-y-1">
          <SectionTitle id="site-import-dropped">{sr.dropped}</SectionTitle>
          <ul className="list-disc pl-5 text-sm" data-site-import-dropped="">
            {report.dropped.map((d) => (
              <li key={`${d.page}/${d.permission}/${d.subject}`}>
                {d.page ? sr.droppedOnPage(sr.listNames[d.permission] ?? d.permission, d.subject, d.page) : `${d.permission}: ${d.subject}`}
              </li>
            ))}
          </ul>
        </section>
      )}
    </Card>
  );
}

/** What an organization's archive leaves out, before anybody exports one, so nobody is surprised where it lands. */
function NotCarried() {
  return (
    <section aria-labelledby="site-not-carried" className="space-y-2" data-site-not-carried="">
      <SectionTitle id="site-not-carried">{t.siteTransfer.notCarriedTitle}</SectionTitle>
      <p className="text-sm text-ink-muted">{t.siteTransfer.notCarriedHint}</p>
      <ul className="list-disc space-y-1 pl-5 text-sm">
        {Object.entries(t.siteTransfer.notCarried).map(([key, o]) => (
          <li key={key} data-not-carried={key}>
            <span className="font-medium text-ink">{o.what}.</span> <span className="text-ink-muted">{o.why}</span>
          </li>
        ))}
      </ul>
    </section>
  );
}
