import { useState, type FormEvent } from "react";
import { ApiError } from "@/api/client";
import { useHub, useSetHub } from "@/api/hub";
import { useSpaces } from "@/api/spaces";
import { useOutline } from "@/api/tree";
import { Button, Checkbox, ErrorBanner, PageHeader, Select, Skeleton } from "@/components/ui";
import { PageLink } from "@/features/pages/PageLink";
import { t } from "@/i18n";

const INDENT = "   ";

/** Where an administrator chooses the organization's hub page and whether everybody lands on it. */
export function HubSettings() {
  const { data: hub, isLoading } = useHub();
  const { data: spaces } = useSpaces();
  const save = useSetHub();
  const [spaceKey, setSpaceKey] = useState<string>();
  const [pageId, setPageId] = useState<string>();
  const [landing, setLanding] = useState<boolean>();
  const shownSpace = spaceKey ?? hub?.page?.spaceKey ?? spaces?.[0]?.key ?? "";
  const { data: outline, isLoading: outlineLoading } = useOutline(shownSpace);
  const pages = outline ?? [];
  const shownPage = pages.some((entry) => entry.id === (pageId ?? hub?.page?.id)) ? (pageId ?? hub?.page?.id ?? "") : (pages[0]?.id ?? "");
  const shownLanding = landing ?? hub?.landing ?? false;
  const fields = save.error instanceof ApiError ? save.error.fields : {};

  function submit(event: FormEvent) {
    event.preventDefault();
    if (!shownPage) return;
    save.mutate({ pageId: shownPage, landing: shownLanding }, { onSuccess: () => setPageId(undefined) });
  }

  return (
    <div className="mx-auto max-w-3xl" data-hub-settings="">
      <PageHeader crumb={t.settings.title} title={t.hub.title} />
      <p className="mb-4 text-sm text-ink-muted">{t.hub.intro}</p>
      {isLoading && <Skeleton />}
      {hub && (
        <p className="mb-4 text-sm text-ink" data-hub-current="">
          {hub.page ? (
            <>
              {t.hub.current}{" "}
              <PageLink spaceKey={hub.page.spaceKey} id={hub.page.id} title={hub.page.title} className="font-medium text-accent hover:underline">
                {hub.page.title}
              </PageLink>
              {hub.landing && <span className="text-ink-muted"> {t.hub.everybodyLands}</span>}
            </>
          ) : (
            t.hub.none
          )}
        </p>
      )}
      <form onSubmit={submit} className="space-y-3">
        {save.error && !Object.keys(fields).length && <ErrorBanner>{save.error.message}</ErrorBanner>}
        <Select
          label={t.page.space}
          value={shownSpace}
          onChange={(event) => {
            setSpaceKey(event.target.value);
            setPageId(undefined);
          }}
        >
          {(spaces ?? []).map((space) => (
            <option key={space.key} value={space.key}>
              {space.name} ({space.key})
            </option>
          ))}
        </Select>
        <Select
          label={t.hub.page}
          value={shownPage}
          onChange={(event) => setPageId(event.target.value)}
          disabled={outlineLoading}
          hint={outlineLoading ? t.page.loadingPlaces : undefined}
          error={fields.pageId}
        >
          {pages.map((entry) => (
            <option key={entry.id} value={entry.id}>
              {INDENT.repeat(entry.depth) + entry.title}
            </option>
          ))}
        </Select>
        <Checkbox label={t.hub.landing} checked={shownLanding} onChange={(event) => setLanding(event.target.checked)} />
        <div className="flex flex-wrap gap-2 pt-1">
          <Button type="submit" loading={save.isPending} disabled={!shownPage} data-action="save-hub">
            {t.hub.save}
          </Button>
          {hub?.page && (
            <Button
              type="button"
              variant="secondary"
              onClick={() => save.mutate({ pageId: null, landing: false }, { onSuccess: () => setLanding(undefined) })}
              data-action="clear-hub"
            >
              {t.hub.clear}
            </Button>
          )}
        </div>
      </form>
    </div>
  );
}
