import { Link, useNavigate } from "@tanstack/react-router";
import { useCreateExampleSpace, useExampleSpace, type ExampleSpaceResult } from "@/api/exampleSpace";
import type { Space } from "@/api/spaces";
import { Button, EmptyState, ErrorBanner, PageHeader, Skeleton } from "@/components/ui";
import { Icon } from "@/components/icons";
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

/** Makes the example and opens it, or, when it was there already, says so where it is. */
function useMakeExample() {
  const navigate = useNavigate();
  const make = useCreateExampleSpace();
  const run = () =>
    make.mutate(undefined, {
      onSuccess: ({ space, created }: ExampleSpaceResult) => {
        if (created) void navigate({ to: "/s/$spaceKey", params: { spaceKey: space.key } });
      },
    });
  const found = make.data && !make.data.created ? make.data.space : undefined;
  return { make, run, found };
}

/** The spaces overview's offer of the example space, to administrators while there is none. */
export function ExampleSpaceButton() {
  const admin = useCanAdministerOrg();
  const { data: example, isSuccess } = useExampleSpace(admin);
  const { make, run, found } = useMakeExample();
  if (found) return <ExistingExample space={found} />;
  if (!admin || !isSuccess || example) return null;
  return (
    <>
      <Button variant="secondary" icon={<Icon.Seal />} onClick={run} disabled={make.isPending} data-action="create-example-space">
        {make.isPending ? t.exampleSpace.creating : t.exampleSpace.create}
      </Button>
      {make.error && <ErrorBanner>{make.error.message}</ErrorBanner>}
    </>
  );
}

/** The organization settings' page for the example space. */
export function ExampleSpaceSettings() {
  const admin = useCanAdministerOrg();
  const { data: example, isLoading, error } = useExampleSpace(admin);
  const { make, run, found } = useMakeExample();
  if (!admin) {
    return (
      <div className="mx-auto max-w-3xl">
        <PageHeader crumb={t.settings.title} title={t.exampleSpace.title} />
        <EmptyState icon={<Icon.Lock />} title={t.exampleSpace.title} description={t.exampleSpace.notAdmin} />
      </div>
    );
  }
  const shown = found ?? example;
  return (
    <div className="mx-auto max-w-3xl space-y-4" data-example-space-settings="">
      <PageHeader crumb={t.settings.title} title={t.exampleSpace.title} />
      <p className="text-sm text-ink-muted">{t.exampleSpace.intro}</p>
      {isLoading && <Skeleton />}
      {error && <ErrorBanner>{error.message}</ErrorBanner>}
      {make.error && <ErrorBanner>{make.error.message}</ErrorBanner>}
      {shown && <ExistingExample space={shown} />}
      <Button icon={<Icon.Seal />} onClick={run} disabled={make.isPending || isLoading} data-action="create-example-space">
        {make.isPending ? t.exampleSpace.creating : t.exampleSpace.create}
      </Button>
    </div>
  );
}
