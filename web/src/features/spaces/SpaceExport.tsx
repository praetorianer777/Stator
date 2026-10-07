import { useState } from "react";
import { spaceExportHref, transferOpen, useCreateSpaceExport, useSpaceExports, type ExportFormat, type SpaceExport } from "@/api/spaceTransfers";
import type { Space } from "@/api/spaces";
import { formatSize } from "@/api/attachments";
import { Button, ButtonLink, Card, ErrorBanner, OptionCard, SectionTitle, Skeleton } from "@/components/ui";
import { Icon } from "@/components/icons";
import { language, t } from "@/i18n";

const FORMATS: ExportFormat[] = ["archive", "html"];

/** A space's export for its administrators: which format, the jobs under way, and their files. */
export function SpaceExportPanel({ space }: { space: Space }) {
  const [format, setFormat] = useState<ExportFormat>("archive");
  const exports = useSpaceExports(space.key, space.can.administer);
  const create = useCreateSpaceExport(space.key);
  if (!space.can.administer) return <p className="text-sm text-ink-muted">{t.spaceTransfer.notAdmin}</p>;
  const underWay = create.isPending || (exports.data?.some(transferOpen) ?? false);
  return (
    <div className="space-y-6" data-space-export="">
      <p className="text-sm text-ink-muted">{t.spaceTransfer.exportIntro}</p>
      <div role="radiogroup" aria-label={t.spaceTransfer.format} className="grid gap-3 sm:grid-cols-2">
        {FORMATS.map((f) => (
          <OptionCard
            key={f}
            checked={format === f}
            onSelect={() => setFormat(f)}
            title={t.spaceTransfer.formats[f].title}
            description={t.spaceTransfer.formats[f].description}
            data-export-format={f}
          />
        ))}
      </div>
      {create.error && <ErrorBanner>{create.error.message}</ErrorBanner>}
      <Button icon={<Icon.Download />} loading={create.isPending} disabled={underWay} onClick={() => create.mutate(format)} data-action="export-space">
        {underWay ? t.spaceTransfer.exporting : t.spaceTransfer.export}
      </Button>
      <section aria-labelledby="space-exports-title" className="space-y-2">
        <SectionTitle id="space-exports-title">{t.spaceTransfer.recent}</SectionTitle>
        {exports.isLoading && <Skeleton lines={2} />}
        {exports.error && <ErrorBanner onRetry={() => void exports.refetch()}>{exports.error.message}</ErrorBanner>}
        {exports.data && exports.data.length === 0 && <p className="text-sm text-ink-muted">{t.spaceTransfer.none}</p>}
        <ul className="space-y-2">
          {exports.data?.map((job) => (
            <ExportRow key={job.id} job={job} />
          ))}
        </ul>
      </section>
    </div>
  );
}

function ExportRow({ job }: { job: SpaceExport }) {
  const when = new Intl.DateTimeFormat(language(), { dateStyle: "medium", timeStyle: "short" });
  const percent = job.progress.total > 0 ? Math.round((job.progress.done / job.progress.total) * 100) : 0;
  return (
    <li data-export-row={job.state} data-export-id={job.id}>
      <Card className="flex flex-wrap items-center justify-between gap-3 p-3">
        <div className="min-w-0 space-y-1">
          <p className="text-sm font-medium text-ink">{t.spaceTransfer.formats[job.format].title}</p>
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
            href={spaceExportHref(job.id)}
            variant="secondary"
            icon={<Icon.Download />}
            download={job.fileName ?? undefined}
            data-action="download-export"
          >
            {t.spaceTransfer.download}
          </ButtonLink>
        )}
      </Card>
    </li>
  );
}
