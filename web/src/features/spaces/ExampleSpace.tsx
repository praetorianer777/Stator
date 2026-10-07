import { useEffect, useState } from "react";
import { useQueryClient } from "@tanstack/react-query";
import { Link, useNavigate } from "@tanstack/react-router";
import { exampleSpaceMade, jobOpen, useCreateExampleSpace, useExampleSpace } from "@/api/exampleSpace";
import type { Space } from "@/api/spaces";
import { Button, EmptyState, ErrorBanner, PageHeader, Skeleton } from "@/components/ui";
import { Icon } from "@/components/icons";
import { EXAMPLE_SPACE_GIVE_UP_MS, EXAMPLE_SPACE_SLOW_MS } from "@/config";
import { useCanAdministerOrg } from "@/features/permissions/access";
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
  const job = query.data?.job;

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
  const admin = useCanAdministerOrg();
  const { query, make, run, found, underWay, failure, note } = useMakeExample(admin);
  if (found) return <ExistingExample space={found} />;
  if (!admin || !query.isSuccess || (query.data.space && !underWay)) return null;
  return (
    <>
      <Button variant="secondary" icon={<Icon.Seal />} onClick={run} disabled={underWay} data-action="create-example-space">
        {underWay ? t.exampleSpace.creating : t.exampleSpace.create}
      </Button>
      <Progress note={note} failure={failure} error={make.error} />
    </>
  );
}

/** The organization settings' page for the example space. */
export function ExampleSpaceSettings() {
  const admin = useCanAdministerOrg();
  const { query, make, run, found, underWay, failure, note } = useMakeExample(admin);
  if (!admin) {
    return (
      <div className="mx-auto max-w-3xl">
        <PageHeader crumb={t.settings.title} title={t.exampleSpace.title} />
        <EmptyState icon={<Icon.Lock />} title={t.exampleSpace.title} description={t.exampleSpace.notAdmin} />
      </div>
    );
  }
  const shown = found ?? query.data?.space ?? undefined;
  return (
    <div className="mx-auto max-w-3xl space-y-4" data-example-space-settings="">
      <PageHeader crumb={t.settings.title} title={t.exampleSpace.title} />
      <p className="text-sm text-ink-muted">{t.exampleSpace.intro}</p>
      {query.isLoading && <Skeleton />}
      {query.error && <ErrorBanner>{query.error.message}</ErrorBanner>}
      {shown && !underWay && <ExistingExample space={shown} />}
      <Progress note={note} failure={shown ? undefined : failure} error={make.error} />
      <Button icon={<Icon.Seal />} onClick={run} disabled={underWay || query.isLoading} data-action="create-example-space">
        {underWay ? t.exampleSpace.creating : t.exampleSpace.create}
      </Button>
    </div>
  );
}
