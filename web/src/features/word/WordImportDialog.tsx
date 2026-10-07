import { useEffect, useRef, useState, type FormEvent } from "react";
import { useQueryClient } from "@tanstack/react-query";
import { formatSize } from "@/api/attachments";
import type { ImportResult } from "@/api/markdown";
import { useImportWord, useWordImport, wordImportMade, wordImportOpen, type WordImport } from "@/api/word";
import { Button, Dialog, ErrorBanner } from "@/components/ui";
import { Icon } from "@/components/icons";
import {
  WORD_FILE_PATTERN,
  WORD_IMPORT_GIVE_UP_MS,
  WORD_IMPORT_MAX_BYTES,
  WORD_IMPORT_SLOW_MS,
  WORD_IMPORTS_MAX_BYTES,
  WORD_IMPORTS_MAX_FILES,
  ZIP_FILE_PATTERN,
} from "@/config";
import { PageLink } from "@/features/pages/PageLink";
import { t } from "@/i18n";

/** Why a choice of files cannot be imported, before anything is sent. */
export function wordImportProblem(files: File[]): string | undefined {
  const documents = files.filter((f) => WORD_FILE_PATTERN.test(f.name) || ZIP_FILE_PATTERN.test(f.name));
  if (documents.length === 0) return t.wordImport.noWord;
  if (documents.length > WORD_IMPORTS_MAX_FILES) return t.wordImport.tooMany(WORD_IMPORTS_MAX_FILES);
  if (files.length === 1 && WORD_FILE_PATTERN.test(files[0]!.name) && files[0]!.size > WORD_IMPORT_MAX_BYTES) {
    return t.wordImport.tooLarge(formatSize(WORD_IMPORT_MAX_BYTES));
  }
  if (files.reduce((sum, f) => sum + f.size, 0) > WORD_IMPORTS_MAX_BYTES) return t.wordImport.tooLargeAll(formatSize(WORD_IMPORTS_MAX_BYTES));
  return undefined;
}

function Warnings({ warnings }: { warnings: string[] }) {
  if (warnings.length === 0) return null;
  return (
    <ul className="mt-1 list-disc space-y-1 pl-5 text-xs text-ink-muted" data-import-warnings="">
      {warnings.map((w) => (
        <li key={w}>{w}</li>
      ))}
    </ul>
  );
}

/** One document imported at once: its page, and what did not come across. */
function SingleResult({ result, spaceKey }: { result: ImportResult; spaceKey: string }) {
  const made = result.pages[0];
  return (
    <>
      <p role="status" className="text-sm text-ink" data-import-result="">
        {t.wordImport.imported}
        {made && (
          <>
            {": "}
            <PageLink spaceKey={spaceKey} id={made.id} title={made.title} className="font-medium text-accent hover:underline" />
          </>
        )}
      </p>
      {result.warnings.length > 0 && (
        <>
          <h3 className="mt-3 text-sm font-medium text-ink">{t.wordImport.warningsTitle}</h3>
          <Warnings warnings={result.warnings} />
        </>
      )}
    </>
  );
}

/**
 * Follows an import the worker runs: how far it has come, then what became
 * of each file. Says when the worker seems not to run, and stops asking
 * after a while.
 */
function Followed({ queued, spaceKey }: { queued: WordImport; spaceKey: string }) {
  const queryClient = useQueryClient();
  const [since] = useState(() => Date.now());
  const [slow, setSlow] = useState(false);
  const [gaveUp, setGaveUp] = useState(false);
  const query = useWordImport(queued.id, !gaveUp);
  const job = query.data ?? queued;
  const open = wordImportOpen(job);

  useEffect(() => {
    if (!open) return;
    const left = since - Date.now();
    const timers = [setTimeout(() => setSlow(true), left + WORD_IMPORT_SLOW_MS), setTimeout(() => setGaveUp(true), left + WORD_IMPORT_GIVE_UP_MS)];
    return () => timers.forEach(clearTimeout);
  }, [open, since]);

  const finished = !open;
  useEffect(() => {
    if (finished) void wordImportMade(queryClient);
  }, [finished, queryClient]);

  if (open) {
    let note = job.state === "queued" ? t.wordImport.queued : t.wordImport.running(job.done, job.total);
    if (gaveUp) note = t.wordImport.gaveUp;
    else if (slow && job.state === "queued") note = t.wordImport.slow;
    return (
      <div className="space-y-2" data-word-import={job.state}>
        <p role="status" className="text-sm text-ink" data-import-progress="">
          {note}
        </p>
        <progress className="h-1.5 w-full accent-accent" max={Math.max(job.total, 1)} value={job.done} aria-label={t.wordImport.progress} />
        {query.error && <ErrorBanner onRetry={() => void query.refetch()}>{query.error.message}</ErrorBanner>}
      </div>
    );
  }
  const made = job.files.filter((f) => f.page).length;
  return (
    <div data-word-import={job.state}>
      {job.state === "failed" && job.failure ? (
        <ErrorBanner>{t.wordImport.failures[job.failure]}</ErrorBanner>
      ) : (
        <p role="status" className="text-sm text-ink" data-import-result="">
          {t.wordImport.done(made)}
        </p>
      )}
      <ul className="mt-3 space-y-2 text-sm" data-import-files="">
        {job.files.map((f) => (
          <li key={f.path} style={{ paddingLeft: `${((f.page?.depth ?? 1) - 1) * 1}rem` }} data-imported-file={f.path}>
            {f.page && job.state === "done" ? (
              <PageLink spaceKey={spaceKey} id={f.page.id} title={f.page.title} className="text-accent hover:underline" />
            ) : (
              <span className="text-ink">{f.page?.title ?? f.path}</span>
            )}
            <span className="ml-2 text-xs text-ink-muted">{f.path}</span>
            {f.error && (
              <p className="text-xs text-danger">
                {t.wordImport.notImported}: {f.error}
              </p>
            )}
            <Warnings warnings={f.warnings} />
          </li>
        ))}
      </ul>
      {job.skipped.length > 0 && (
        <>
          <h3 className="mt-3 text-sm font-medium text-ink">{t.wordImport.skipped}</h3>
          <ul className="mt-1 list-disc pl-5 text-xs text-ink-muted" data-import-skipped="">
            {job.skipped.map((name) => (
              <li key={name}>{name}</li>
            ))}
          </ul>
        </>
      )}
    </div>
  );
}

/** Takes Word documents, or a .zip of them, and makes pages of them under a page. */
export function WordImportDialog({ parent, spaceKey, onClose }: { parent: { id: string; title: string }; spaceKey: string; onClose: () => void }) {
  const importing = useImportWord(parent.id);
  const [files, setFiles] = useState<File[]>([]);
  const [problem, setProblem] = useState<string>();
  const input = useRef<HTMLInputElement>(null);

  function choose(list: FileList | null) {
    setFiles([...(list ?? [])]);
    setProblem(undefined);
    importing.reset();
  }

  function submit(event: FormEvent) {
    event.preventDefault();
    const found = wordImportProblem(files);
    setProblem(found);
    if (!found) importing.mutate(files);
  }

  const answer = importing.data;
  if (answer) {
    return (
      <Dialog title={t.wordImport.title(parent.title)} onClose={onClose} data-word-import-dialog="done">
        {answer.result && <SingleResult result={answer.result} spaceKey={spaceKey} />}
        {answer.job && <Followed queued={answer.job} spaceKey={spaceKey} />}
        <div className="flex justify-end pt-3">
          <Button type="button" onClick={onClose} data-action="close-word-import">
            {t.wordImport.close}
          </Button>
        </div>
      </Dialog>
    );
  }

  const error = problem ?? importing.error?.message;
  const percent = Math.round(importing.progress * 100);
  return (
    <Dialog title={t.wordImport.title(parent.title)} onClose={onClose} data-word-import-dialog="">
      <form onSubmit={submit} className="space-y-3" noValidate>
        <p className="text-sm text-ink-muted">{t.wordImport.hint}</p>
        {error && <ErrorBanner>{error}</ErrorBanner>}
        <input
          ref={input}
          type="file"
          multiple
          accept=".docx,.zip,application/vnd.openxmlformats-officedocument.wordprocessingml.document,application/zip"
          className="sr-only"
          tabIndex={-1}
          aria-hidden="true"
          onChange={(event) => choose(event.target.files)}
          data-word-files=""
        />
        <Button type="button" variant="secondary" icon={<Icon.File />} onClick={() => input.current?.click()} data-action="choose-word-files">
          {t.wordImport.choose}
        </Button>
        <div aria-live="polite" className="text-sm text-ink" data-chosen-files={files.length}>
          {files.length === 0 ? (
            <span className="text-ink-muted">{t.wordImport.noneChosen}</span>
          ) : (
            <>
              <span>{t.wordImport.chosen(files.length)}</span>
              <ul className="mt-1 max-h-32 overflow-y-auto text-xs text-ink-muted">
                {files.map((f) => (
                  <li key={f.name}>{f.name}</li>
                ))}
              </ul>
            </>
          )}
        </div>
        {importing.isPending && <progress className="h-1.5 w-full accent-accent" max={100} value={percent} aria-label={t.wordImport.sending(percent)} />}
        <div className="flex justify-end gap-2 pt-1">
          <Button type="button" variant="secondary" onClick={onClose}>
            {t.wordImport.cancel}
          </Button>
          <Button type="submit" loading={importing.isPending} disabled={files.length === 0} icon={<Icon.Upload />} data-action="confirm-word-import">
            {t.wordImport.submit}
          </Button>
        </div>
      </form>
    </Dialog>
  );
}
