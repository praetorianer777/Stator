import { useEffect, useId, useRef, useState, type FormEvent } from "react";
import { formatSize } from "@/api/attachments";
import { markdownExportHref, uploadPath, useImportMarkdown, type ExportScope } from "@/api/markdown";
import { Button, ButtonLink, Dialog, ErrorBanner } from "@/components/ui";
import { Icon } from "@/components/icons";
import { MARKDOWN_FILE_PATTERN, MARKDOWN_IMPORT_MAX_BYTES, ZIP_FILE_PATTERN } from "@/config";
import { PageLink } from "@/features/pages/PageLink";
import { t } from "@/i18n";

const SCOPES: { scope: ExportScope; label: string; hint: string }[] = [
  { scope: "markdown", label: t.markdown.exportMarkdown, hint: t.markdown.exportMarkdownHint },
  { scope: "page", label: t.markdown.exportPage, hint: t.markdown.exportPageHint },
  { scope: "subtree", label: t.markdown.exportSubtree, hint: t.markdown.exportSubtreeHint },
];

/** Offers a page as Markdown: alone, with its files, or with the pages below it, as a download. */
export function ExportDialog({ page, onClose }: { page: { id: string; title: string }; onClose: () => void }) {
  const [scope, setScope] = useState<ExportScope>("page");
  const name = useId();
  return (
    <Dialog title={t.markdown.exportTitle(page.title)} onClose={onClose} data-markdown-export="">
      <fieldset className="space-y-2">
        <legend className="mb-2 text-sm font-medium text-ink">{t.markdown.exportWhat}</legend>
        {SCOPES.map((each) => (
          <label key={each.scope} className="flex items-start gap-2 text-sm text-ink">
            <input
              type="radio"
              name={name}
              value={each.scope}
              checked={scope === each.scope}
              onChange={() => setScope(each.scope)}
              className="mt-0.5 size-4 accent-accent"
              data-export-scope={each.scope}
            />
            <span>
              <span className="block">{each.label}</span>
              <span className="block text-xs text-ink-muted">{each.hint}</span>
            </span>
          </label>
        ))}
      </fieldset>
      <p className="mt-3 text-xs text-ink-muted">{t.markdown.exportNote}</p>
      <div className="flex justify-end gap-2 pt-3">
        <Button type="button" variant="secondary" onClick={onClose}>
          {t.markdown.cancel}
        </Button>
        <ButtonLink variant="primary" href={markdownExportHref(page.id, scope)} download onClick={onClose} icon={<Icon.Download />} data-action="download-markdown">
          {t.markdown.download}
        </ButtonLink>
      </div>
    </Dialog>
  );
}

/** Why a choice of files cannot be imported, before anything is sent. */
export function importProblem(files: File[]): string | undefined {
  if (!files.some((f) => MARKDOWN_FILE_PATTERN.test(f.name) || ZIP_FILE_PATTERN.test(f.name))) return t.markdown.noMarkdown;
  if (files.reduce((sum, f) => sum + f.size, 0) > MARKDOWN_IMPORT_MAX_BYTES) return t.markdown.tooLarge(formatSize(MARKDOWN_IMPORT_MAX_BYTES));
  return undefined;
}

/** Takes Markdown files, a folder or a .zip and makes pages of them under a page. */
export function ImportDialog({ parent, spaceKey, onClose }: { parent: { id: string; title: string }; spaceKey: string; onClose: () => void }) {
  const importing = useImportMarkdown(parent.id);
  const [files, setFiles] = useState<File[]>([]);
  const [problem, setProblem] = useState<string>();
  const filesInput = useRef<HTMLInputElement>(null);
  const folderInput = useRef<HTMLInputElement>(null);
  // React has no prop for it, and without it the picker takes files, not a folder.
  useEffect(() => folderInput.current?.setAttribute("webkitdirectory", ""), []);

  function choose(list: FileList | null) {
    const chosen = [...(list ?? [])];
    setFiles(chosen);
    setProblem(undefined);
    importing.reset();
  }

  function submit(event: FormEvent) {
    event.preventDefault();
    const found = importProblem(files);
    setProblem(found);
    if (!found) importing.mutate(files);
  }

  const result = importing.data;
  if (result) {
    return (
      <Dialog title={t.markdown.importTitle(parent.title)} onClose={onClose} data-markdown-import="done">
        <p role="status" className="text-sm text-ink" data-import-result="">
          {t.markdown.imported(result.pages.length)}
        </p>
        <h3 className="mt-3 text-sm font-medium text-ink">{t.markdown.importedPages}</h3>
        <ul className="mt-1 space-y-1 text-sm">
          {result.pages.map((p) => (
            <li key={p.id} style={{ paddingLeft: `${(p.depth - 1) * 1}rem` }} data-imported-page={p.title}>
              <PageLink spaceKey={spaceKey} id={p.id} title={p.title} className="text-accent hover:underline" />
            </li>
          ))}
        </ul>
        {result.warnings.length > 0 && (
          <>
            <h3 className="mt-3 text-sm font-medium text-ink">{t.markdown.warningsTitle}</h3>
            <ul className="mt-1 list-disc space-y-1 pl-5 text-xs text-ink-muted" data-import-warnings="">
              {result.warnings.map((w) => (
                <li key={w}>{w}</li>
              ))}
            </ul>
          </>
        )}
        <div className="flex justify-end pt-3">
          <Button type="button" onClick={onClose} data-action="close-import">
            {t.markdown.close}
          </Button>
        </div>
      </Dialog>
    );
  }

  const error = problem ?? importing.error?.message;
  return (
    <Dialog title={t.markdown.importTitle(parent.title)} onClose={onClose} data-markdown-import="">
      <form onSubmit={submit} className="space-y-3" noValidate>
        <p className="text-sm text-ink-muted">{t.markdown.importHint}</p>
        {error && <ErrorBanner>{error}</ErrorBanner>}
        <input
          ref={filesInput}
          type="file"
          multiple
          className="sr-only"
          tabIndex={-1}
          aria-hidden="true"
          onChange={(event) => choose(event.target.files)}
          data-markdown-files=""
        />
        <input
          ref={folderInput}
          type="file"
          multiple
          className="sr-only"
          tabIndex={-1}
          aria-hidden="true"
          onChange={(event) => choose(event.target.files)}
          data-markdown-folder=""
        />
        <div className="flex flex-wrap gap-2">
          <Button type="button" variant="secondary" icon={<Icon.File />} onClick={() => filesInput.current?.click()} data-action="choose-markdown-files">
            {t.markdown.chooseFiles}
          </Button>
          <Button type="button" variant="secondary" icon={<Icon.Space />} onClick={() => folderInput.current?.click()} data-action="choose-markdown-folder">
            {t.markdown.chooseFolder}
          </Button>
        </div>
        <div aria-live="polite" className="text-sm text-ink" data-chosen-files={files.length}>
          {files.length === 0 ? (
            <span className="text-ink-muted">{t.markdown.noneChosen}</span>
          ) : (
            <>
              <span>{t.markdown.chosen(files.length)}</span>
              <ul className="mt-1 max-h-32 overflow-y-auto text-xs text-ink-muted">
                {files.map((f) => (
                  <li key={uploadPath(f)}>{uploadPath(f)}</li>
                ))}
              </ul>
            </>
          )}
        </div>
        {importing.isPending && (
          <progress
            className="h-1.5 w-full accent-accent"
            max={100}
            value={Math.round(importing.progress * 100)}
            aria-label={t.markdown.sending(Math.round(importing.progress * 100))}
          />
        )}
        <div className="flex justify-end gap-2 pt-1">
          <Button type="button" variant="secondary" onClick={onClose}>
            {t.markdown.cancel}
          </Button>
          <Button type="submit" loading={importing.isPending} disabled={files.length === 0} icon={<Icon.Upload />} data-action="confirm-import">
            {t.markdown.importSubmit}
          </Button>
        </div>
      </form>
    </Dialog>
  );
}
