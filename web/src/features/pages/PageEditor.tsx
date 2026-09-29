import { useState, type FormEvent } from "react";
import { useNavigate } from "@tanstack/react-router";
import { usePage, useUpdatePage, type Page } from "@/api/pages";
import type { Space } from "@/api/spaces";
import { Button, ErrorBanner, Field, PageHeader, Skeleton } from "@/components/ui";
import { Editor } from "@/features/editor/Editor";
import { emptyDoc, type Doc } from "@/features/editor/schema";
import { PAGE_TITLE_MAX_LENGTH } from "@/config";
import { t } from "@/i18n";
import { pageSlug } from "@/lib/slug";
import { pageCrumbs } from "./PageScreen";

/**
 * Edits a page's title and body and saves them straight over the page, naming
 * the version it started from so a save over somebody else's is refused.
 */
export function PageEditor({ pageId }: { pageId: string }) {
  const { data, isLoading, error, refetch } = usePage(pageId);
  if (error) return <ErrorBanner onRetry={() => void refetch()}>{error.message}</ErrorBanner>;
  if (isLoading || !data) return <Skeleton />;
  // Keyed by the page alone: a save changes the version, and a form started
  // afresh would drop the save's own callback, which leaves the editor.
  return <PageForm key={data.page.id} page={data.page} space={data.space} />;
}

// The header's buttons are drawn in the shell's strip, outside the form, so
// they name it.
const PAGE_FORM_ID = "page-form";

function PageForm({ page, space }: { page: Page; space: Space }) {
  const navigate = useNavigate();
  const update = useUpdatePage(page.id);
  const [title, setTitle] = useState(page.title);
  const [body, setBody] = useState<Doc | null>(page.body);
  const [titleError, setTitleError] = useState("");

  const view = (saved: Page) =>
    saved.home
      ? navigate({ to: "/s/$spaceKey", params: { spaceKey: space.key } })
      : navigate({ to: "/s/$spaceKey/p/$pageId/$slug", params: { spaceKey: space.key, pageId: saved.id, slug: pageSlug(saved.title) } });

  function save(event?: FormEvent) {
    event?.preventDefault();
    if (!title.trim()) {
      setTitleError(t.page.emptyTitle);
      return;
    }
    setTitleError("");
    update.mutate({ title, body: body ?? emptyDoc, version: page.version }, { onSuccess: (saved) => void view(saved) });
  }

  return (
    <form id={PAGE_FORM_ID} onSubmit={save} className="mx-auto max-w-3xl space-y-4" data-page-editor={page.id}>
      <PageHeader
        crumbs={pageCrumbs(space, page)}
        title={t.page.editing(page.title)}
        actions={
          <>
            <Button type="button" variant="secondary" onClick={() => void view(page)} data-action="cancel-edit">
              {t.page.cancel}
            </Button>
            <Button type="submit" form={PAGE_FORM_ID} loading={update.isPending} data-action="save-page">
              {t.page.save}
            </Button>
          </>
        }
      />
      {update.error && <ErrorBanner>{update.error.message}</ErrorBanner>}
      <Field
        label={t.page.title}
        value={title}
        maxLength={PAGE_TITLE_MAX_LENGTH}
        onChange={(event) => setTitle(event.target.value)}
        error={titleError}
        controlSize="lg"
      />
      <Editor id="page-body" value={page.body} onChange={setBody} onSubmit={() => save()} />
    </form>
  );
}
