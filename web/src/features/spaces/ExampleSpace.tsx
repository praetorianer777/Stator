import { useEffect, useState } from "react";
import { useQueryClient } from "@tanstack/react-query";
import { Link, useNavigate } from "@tanstack/react-router";
import {
  type ExampleSpaceReport,
  exampleSpaceMade,
  jobOpen,
  renewable,
  useCreateExampleSpace,
  useExampleSpace,
  useRenewExampleSpace,
} from "@/api/exampleSpace";
import type { Space } from "@/api/spaces";
import { Page, PageHeader, Button, ErrorBanner, Segmented, Skeleton } from "@/components/ui";
import { Icon } from "@/components/icons";
import { EXAMPLE_SPACE_GIVE_UP_MS, EXAMPLE_SPACE_SLOW_MS } from "@/config";
import { PageLink } from "@/features/pages/PageLink";
import { useCanAdministerSite } from "@/features/permissions/access";
import { t } from "@/i18n";

/** The example space by its name, linked, with a word when it is archived. */
function ExistingExample({ space }: { space: Space }) {
  return (
    <p className="text-sm text-ink" role="status" data-example-exists="">
      {t.exampleSpace.exists}{" "}
      <Link to="/s/$spaceKey" params={{ spaceKey: space.key }} className="font-medium text-accent hover:underline">
        {space.name}
      </Link>
      {space.archivedAt && <span className="text-ink-muted"> {t.exampleSpace.archived}</span>}
    </p>
  );
}

/**
 * Asks for the example and follows the worker making it: opens it once made,
 * says when the worker seems not to run, and stops asking after a while.
 */
function useMakeExample(admin: boolean) {
  const navigate = useNavigate();
  const queryClient = useQueryClient();
  const [followed, setFollowed] = useState<{ id: string; since: number } | null>(null);
  const [slow, setSlow] = useState(false);
  const [gaveUp, setGaveUp] = useState(false);
  const query = useExampleSpace(admin, !gaveUp);
  const make = useCreateExampleSpace();
  // A renewal is followed where it is offered; here only a making counts.
  const job = query.data?.job?.kind === "make" ? query.data.job : undefined;

  useEffect(() => {
    if (jobOpen(job) && job.id !== followed?.id) {
      setFollowed({ id: job.id, since: Date.now() });
      setSlow(false);
      setGaveUp(false);
    }
  }, [job, followed]);

  useEffect(() => {
    if (!followed) return;
    const left = followed.since - Date.now();
    const timers = [setTimeout(() => setSlow(true), left + EXAMPLE_SPACE_SLOW_MS), setTimeout(() => setGaveUp(true), left + EXAMPLE_SPACE_GIVE_UP_MS)];
    return () => timers.forEach(clearTimeout);
  }, [followed]);

  // Only a making this page saw under way opens the space; one finished
  // before it came is named, not jumped to.
  const madeKey = job && job.id === followed?.id && job.state === "done" ? job.spaceKey : null;
  useEffect(() => {
    if (!madeKey) return;
    void exampleSpaceMade(queryClient);
    void navigate({ to: "/s/$spaceKey", params: { spaceKey: madeKey } });
  }, [madeKey, navigate, queryClient]);

  const run = () => make.mutate();
  const found = make.data?.space && !make.data.job ? make.data.space : undefined;
  const underWay = make.isPending || jobOpen(job) || !!madeKey;
  const failure = job?.state === "failed" && job.failure ? t.exampleSpace.failures[job.failure] : undefined;
  let note: string | undefined;
  if (underWay) note = gaveUp ? t.exampleSpace.gaveUp : slow && job?.state === "queued" ? t.exampleSpace.slow : t.exampleSpace.underWay;
  return { query, make, run, found, underWay, failure, note };
}

/** Says how the making goes, or why it failed. */
function Progress({ note, failure, error }: { note?: string; failure?: string; error?: Error | null }) {
  return (
    <>
      {note && (
        <p className="text-sm text-ink-muted" role="status" data-example-progress="">
          {note}
        </p>
      )}
      {!note && failure && <ErrorBanner>{failure}</ErrorBanner>}
      {error && <ErrorBanner>{error.message}</ErrorBanner>}
    </>
  );
}

/** The spaces overview's offer of the example space, to administrators while there is none. */
export function ExampleSpaceButton() {
  const admin = useCanAdministerSite();
  const { query, make, run, found, underWay, failure, note } = useMakeExample(admin);
  if (found) return <ExistingExample space={found} />;
  if (query.data?.space && !underWay) return <ExampleRenewal />;
  if (!admin || !query.isSuccess) return null;
  return (
    <>
      <Button variant="secondary" icon={<Icon.Seal />} onClick={run} disabled={underWay} data-action="create-example-space">
        {underWay ? t.exampleSpace.creating : t.exampleSpace.create}
      </Button>
      <Progress note={note} failure={failure} error={make.error} />
    </>
  );
}

/** The Admin page for the example space; the Admin area lets only administrators in. */
export function ExampleSpaceSettings() {
  const { query, make, run, found, underWay, failure, note } = useMakeExample(true);
  const shown = found ?? query.data?.space ?? undefined;
  return (
    <Page width="narrow" className="space-y-4" data-example-space-settings="">
      <PageHeader title={t.exampleSpace.title} />
      <p className="text-sm text-ink-muted">{t.exampleSpace.intro}</p>
      {query.isLoading && <Skeleton />}
      {query.error && <ErrorBanner>{query.error.message}</ErrorBanner>}
      {shown && !underWay && <ExistingExample space={shown} />}
      {shown && !underWay && <ExampleRenewal choose />}
      <Progress note={note} failure={shown ? undefined : failure} error={make.error} />
      <Button icon={<Icon.Seal />} onClick={run} disabled={underWay || query.isLoading} data-action="create-example-space">
        {underWay ? t.exampleSpace.creating : t.exampleSpace.create}
      </Button>
    </Page>
  );
}

const LANGUAGES = ["en", "de"] as const;
type Language = (typeof LANGUAGES)[number];

/** The pages of a renewal's report under one heading, linked. */
function ReportList({ spaceKey, heading, pages, attr }: { spaceKey: string; heading: string; pages: ExampleSpaceReport["added"]; attr: string }) {
  if (pages.length === 0) return null;
  return (
    <div {...{ [attr]: "" }}>
      <p className="font-medium text-ink">{heading}</p>
      <ul className="list-disc ps-5">
        {pages.map((page) => (
          <li key={page.id}>
            <PageLink spaceKey={spaceKey} id={page.id} title={page.title} className="text-accent hover:underline" />
          </li>
        ))}
      </ul>
    </div>
  );
}

/**
 * Tells administrators a newer example space is there and what it brings,
 * renews it, follows the worker doing so, and says what it added and kept.
 */
export function ExampleRenewal({ choose = false, spaceId }: { choose?: boolean; spaceId?: string }) {
  const admin = useCanAdministerSite();
  const queryClient = useQueryClient();
  const query = useExampleSpace(admin);
  const renew = useRenewExampleSpace();
  const [followed, setFollowed] = useState<string | null>(null);
  const [picked, setPicked] = useState<Language | null>(null);
  const answer = query.data;
  const job = answer?.job?.kind === "renew" ? answer.job : undefined;
  const asked = renew.data?.job?.id;

  useEffect(() => {
    if (asked) setFollowed(asked);
  }, [asked]);

  const mine = job && job.id === followed ? job : undefined;
  const renewed = mine?.state === "done";
  useEffect(() => {
    // The worker published the space's pages anew; whatever was read of them is old.
    if (renewed) void queryClient.invalidateQueries();
  }, [renewed, queryClient]);

  const space = answer?.space;
  const edition = answer?.edition;
  if (!admin || !space || (spaceId && space.id !== spaceId)) return null;
  const underWay = renew.isPending || jobOpen(job);
  if (!renewable(edition) && !underWay && !mine) return null;
  const lang = picked ?? (edition?.language as Language | undefined) ?? "en";
  const failure = mine?.state === "failed" && mine.failure ? t.exampleSpace.renewal.failures[mine.failure] : undefined;
  const report = renewed ? mine.report : null;

  return (
    <section className="mb-4 space-y-2 rounded-control border border-border bg-surface-raised px-4 py-3 text-sm text-ink" data-example-renewal="">
      {report ? (
        <>
          <p className="font-medium" role="status" data-example-renewed="">
            {t.exampleSpace.renewal.done}
          </p>
          <ReportList spaceKey={space.key} heading={t.exampleSpace.renewal.added} pages={report.added} attr="data-renewal-added" />
          <ReportList spaceKey={space.key} heading={t.exampleSpace.renewal.remade} pages={report.remade} attr="data-renewal-remade" />
          <ReportList spaceKey={space.key} heading={t.exampleSpace.renewal.kept} pages={report.kept} attr="data-renewal-kept" />
          <ReportList spaceKey={space.key} heading={t.exampleSpace.renewal.retired} pages={report.retired} attr="data-renewal-retired" />
        </>
      ) : (
        <>
          <p className="font-medium">{t.exampleSpace.renewal.available}</p>
          {edition && edition.new.length > 0 && (
            <ul className="list-disc ps-5 text-ink-muted" data-example-new="">
              {edition.new.map((line) => (
                <li key={line}>{line}</li>
              ))}
            </ul>
          )}
          <p className="text-ink-muted">{t.exampleSpace.renewal.intro}</p>
          {choose && (
            <Segmented<Language>
              label={t.exampleSpace.renewal.language}
              value={lang}
              onChange={setPicked}
              size="sm"
              options={LANGUAGES.map((each) => ({ value: each, label: t.exampleSpace.renewal.languages[each], attrs: { "data-renewal-language": each } }))}
            />
          )}
          <div className="flex flex-wrap items-center gap-3">
            <Button
              size="sm"
              icon={<Icon.Seal />}
              onClick={() => renew.mutate(choose ? lang : undefined)}
              disabled={underWay}
              data-action="renew-example-space"
            >
              {underWay ? t.exampleSpace.renewal.renewing : t.exampleSpace.renewal.renew}
            </Button>
            {underWay && (
              <span className="text-ink-muted" role="status" data-example-progress="">
                {t.exampleSpace.renewal.underWay}
              </span>
            )}
          </div>
          {failure && <ErrorBanner>{failure}</ErrorBanner>}
          {renew.error && <ErrorBanner>{renew.error.message}</ErrorBanner>}
        </>
      )}
    </section>
  );
}
