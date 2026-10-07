import { useRef, useState, type FormEvent } from "react";
import { Link } from "@tanstack/react-router";
import { ApiError } from "@/api/client";
import { transferOpen, useImportSpace, useSpaceImport, type SpaceImportReport } from "@/api/spaceTransfers";
import { Button, Card, ErrorBanner, Field, PageHeader, SectionTitle } from "@/components/ui";
import { Icon } from "@/components/icons";
import { SPACE_KEY_MAX_LENGTH, SPACE_NAME_MAX_LENGTH } from "@/config";
import { useCanCreateSpace } from "@/features/permissions/access";
import { t } from "@/i18n";

/** Makes a new space of an archive: the upload, the worker's progress, and at the end what could not come across. */
export function SpaceImportPage() {
  const mayCreate = useCanCreateSpace();
  const [file, setFile] = useState<File | null>(null);
  const [key, setKey] = useState("");
  const [name, setName] = useState("");
  const [jobId, setJobId] = useState<string>();
  const input = useRef<HTMLInputElement>(null);
  const upload = useImportSpace();
  const job = useSpaceImport(jobId);
  const fields = upload.error instanceof ApiError ? upload.error.fields : {};

  function submit(event: FormEvent) {
    event.preventDefault();
    if (!file) return;
    upload.mutate({ file, key, name }, { onSuccess: (queued) => setJobId(queued.id) });
  }

  function again() {
    setJobId(undefined);
    upload.reset();
  }

  const header = <PageHeader crumbs={[{ label: t.spaces.title, render: (label) => <Link to="/spaces">{label}</Link> }]} title={t.spaceTransfer.importTitle} />;
  if (!mayCreate) {
    return (
      <div className="mx-auto max-w-2xl">
        {header}
        <p className="text-sm text-ink-muted">{t.spaceTransfer.notCreator}</p>
      </div>
    );
  }
  const current = job.data;
  if (jobId) {
    const percent = current && current.progress.total > 0 ? Math.round((current.progress.done / current.progress.total) * 100) : 0;
    return (
      <div className="mx-auto max-w-2xl space-y-4" data-space-import={current?.state ?? "queued"}>
        {header}
        {job.error && <ErrorBanner onRetry={() => void job.refetch()}>{job.error.message}</ErrorBanner>}
        {(!current || transferOpen(current)) && (
          <div className="space-y-2">
            <p className="text-sm text-ink" role="status">
              {current?.state === "running" ? t.spaceTransfer.progress(current.progress.done, current.progress.total) : t.spaceTransfer.importQueued}
            </p>
            <progress className="h-1.5 w-full accent-accent" max={100} value={percent} aria-label={t.spaceTransfer.progressLabel(percent)} />
          </div>
        )}
        {current?.state === "failed" && (
          <>
            <ErrorBanner>{current.message}</ErrorBanner>
            <Button variant="secondary" onClick={again} data-action="import-again">
              {t.spaceTransfer.tryAgain}
            </Button>
          </>
        )}
        {current?.state === "done" && current.spaceKey && (
          <>
            <p className="text-sm text-ink" role="status" data-import-done="">
              {t.spaceTransfer.imported}{" "}
              <Link to="/s/$spaceKey" params={{ spaceKey: current.spaceKey }} className="font-medium text-accent hover:underline" data-action="open-imported">
                {t.spaceTransfer.open(current.spaceKey)}
              </Link>
            </p>
            {current.report && <ImportReport report={current.report} />}
          </>
        )}
      </div>
    );
  }
  const error = upload.error && !Object.keys(fields).length ? upload.error.message : undefined;
  return (
    <div className="mx-auto max-w-2xl" data-space-import="">
      {header}
      <p className="mb-4 text-sm text-ink-muted">{t.spaceTransfer.importIntro}</p>
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
          data-import-file=""
        />
        <div className="space-y-1">
          <Button type="button" variant="secondary" icon={<Icon.File />} onClick={() => input.current?.click()} data-action="choose-archive">
            {t.spaceTransfer.chooseArchive}
          </Button>
          <p className="text-sm text-ink" aria-live="polite" data-chosen-archive={file?.name ?? ""}>
            {file ? file.name : <span className="text-ink-muted">{t.spaceTransfer.noArchive}</span>}
          </p>
          {fields.file && <p className="text-sm text-danger">{fields.file}</p>}
        </div>
        <Field
          label={t.spaces.key}
          hint={t.spaceTransfer.keyHint}
          value={key}
          maxLength={SPACE_KEY_MAX_LENGTH}
          className="font-mono uppercase"
          onChange={(event) => setKey(event.target.value.toUpperCase())}
          error={fields.key}
        />
        <Field
          label={t.spaces.name}
          hint={t.spaceTransfer.nameHint}
          value={name}
          maxLength={SPACE_NAME_MAX_LENGTH}
          onChange={(event) => setName(event.target.value)}
          error={fields.name}
        />
        {upload.isPending && (
          <progress
            className="h-1.5 w-full accent-accent"
            max={100}
            value={Math.round(upload.progress * 100)}
            aria-label={t.spaceTransfer.sending(Math.round(upload.progress * 100))}
          />
        )}
        <Button type="submit" icon={<Icon.Upload />} loading={upload.isPending} disabled={!file} data-action="confirm-import-space">
          {t.spaceTransfer.importSpace}
        </Button>
      </form>
    </div>
  );
}

/** A space permission by the name the permissions page gives it. */
function spaceName(permission: string): string {
  return (t.permissions.spaceNames as Record<string, string>)[permission] ?? permission;
}

/** What the import could not bring across as it was, as the importer reads it at the end. */
function ImportReport({ report }: { report: SpaceImportReport }) {
  const r = t.spaceTransfer.report;
  return (
    <Card className="space-y-4 p-4" data-import-report="">
      <p className="text-sm text-ink">{r.counts(report.pages, report.versions, report.files, report.comments)}</p>
      {report.people.length === 0 && report.groups.length === 0 && report.dropped.length === 0 ? (
        <p className="text-sm text-ink-muted">{r.allFound}</p>
      ) : (
        <>
          {report.people.length > 0 && (
            <section aria-labelledby="import-people" className="space-y-1">
              <SectionTitle id="import-people">{r.people}</SectionTitle>
              <p className="text-xs text-ink-muted">{r.peopleHint}</p>
              <ul className="list-disc pl-5 text-sm" data-import-people="">
                {report.people.map((p) => (
                  <li key={p.email || p.name}>{p.email ? `${p.name} <${p.email}>` : p.name}</li>
                ))}
              </ul>
            </section>
          )}
          {report.groups.length > 0 && (
            <section aria-labelledby="import-groups" className="space-y-1">
              <SectionTitle id="import-groups">{r.groups}</SectionTitle>
              <ul className="list-disc pl-5 text-sm" data-import-groups="">
                {report.groups.map((g) => (
                  <li key={g}>{g}</li>
                ))}
              </ul>
            </section>
          )}
          {report.dropped.length > 0 && (
            <section aria-labelledby="import-dropped" className="space-y-1">
              <SectionTitle id="import-dropped">{r.dropped}</SectionTitle>
              <ul className="list-disc pl-5 text-sm" data-import-dropped="">
                {report.dropped.map((d) => (
                  <li key={`${d.page}/${d.permission}/${d.subject}`}>
                    {d.page
                      ? r.droppedOnPage(r.listNames[d.permission] ?? d.permission, d.subject, d.page)
                      : r.droppedOnSpace(spaceName(d.permission), d.subject)}
                  </li>
                ))}
              </ul>
            </section>
          )}
        </>
      )}
      {(report.reattributed > 0 || report.droppedReactions > 0 || report.mentionsAsText > 0) && (
        <ul className="list-disc pl-5 text-sm text-ink-muted">
          {report.reattributed > 0 && <li>{r.reattributed(report.reattributed)}</li>}
          {report.droppedReactions > 0 && <li>{r.droppedReactions(report.droppedReactions)}</li>}
          {report.mentionsAsText > 0 && <li>{r.mentionsAsText(report.mentionsAsText)}</li>}
        </ul>
      )}
    </Card>
  );
}
