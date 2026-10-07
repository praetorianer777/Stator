import { useState, type ReactNode } from "react";
import { Link, useNavigate } from "@tanstack/react-router";
import { guestSpaceOf, useMe } from "@/api/auth";
import { useSpaces, type Space } from "@/api/spaces";
import { ArchivedMark } from "@/features/archive/ArchiveBanner";
import { useCanCreateSpace } from "@/features/permissions/access";
import { SpaceStar } from "@/features/stars/StarButton";
import { ExampleSpaceButton } from "./ExampleSpace";
import { Button, EmptyState, ErrorBanner, PageHeader, Skeleton, Switch, Table, Td, Th } from "@/components/ui";
import { Icon } from "@/components/icons";
import { t } from "@/i18n";

/**
 * Every space the reader may see, archived ones when asked, the personal ones
 * apart; for administrators the way to make another, and for everybody the way
 * to make their own.
 */
export function SpaceDirectory() {
  const [showArchived, setShowArchived] = useState(false);
  const { data: spaces, isLoading, error, refetch } = useSpaces(showArchived);
  const { data: me } = useMe();
  const mayCreate = useCanCreateSpace();
  const navigate = useNavigate();
  const team = spaces?.filter((space) => !space.owner) ?? [];
  const personal = spaces?.filter((space) => space.owner) ?? [];
  const hasOwn = personal.some((space) => space.owner?.id === me?.user.id);
  const create = mayCreate ? (
    <Button icon={<Icon.Plus />} onClick={() => navigate({ to: "/spaces/new" })} data-action="new-space">
      {t.spaces.create}
    </Button>
  ) : undefined;
  const createOwn =
    spaces && me && !hasOwn && !guestSpaceOf(me) ? (
      <Button variant="secondary" icon={<Icon.User />} onClick={() => navigate({ to: "/spaces/new/personal" })} data-action="new-personal-space">
        {t.spaces.createPersonal}
      </Button>
    ) : undefined;
  const actions = (
    <>
      <span className="inline-flex items-center gap-2 text-sm text-ink-muted">
        <Switch label={t.archive.showArchived} checked={showArchived} onChange={setShowArchived} data-show-archived="" />
        <span aria-hidden="true">{t.archive.showArchived}</span>
      </span>
      <ExampleSpaceButton />
      {mayCreate && (
        <Button variant="secondary" icon={<Icon.Upload />} onClick={() => navigate({ to: "/spaces/import" })} data-action="import-space">
          {t.spaceTransfer.importSpace}
        </Button>
      )}
      {createOwn}
      {create}
    </>
  );

  return (
    <div className="mx-auto max-w-4xl" data-space-directory>
      <PageHeader title={t.spaces.title} actions={actions} />
      {error && <ErrorBanner onRetry={() => void refetch()}>{error.message}</ErrorBanner>}
      {isLoading && <Skeleton />}
      {spaces && team.length === 0 && (
        <EmptyState icon={<Icon.Space />} title={t.spaces.emptyTitle} description={mayCreate ? t.spaces.emptyBody : t.spaces.emptyBodyMember} action={create} />
      )}
      {team.length > 0 && (
        <SpaceTable spaces={team} third={t.spaces.columnDescription} cell={(space) => <span className="text-ink-muted">{space.description}</span>} />
      )}
      {personal.length > 0 && (
        <section className="mt-8" aria-labelledby="personal-spaces" data-personal-spaces="">
          <h2 id="personal-spaces" className="mb-2 text-base font-semibold text-ink">
            {t.spaces.personalTitle}
          </h2>
          <SpaceTable
            spaces={personal}
            third={t.spaces.columnOwner}
            cell={(space) => <span className="text-ink-muted">{space.owner?.id === me?.user.id ? t.spaces.yours : space.owner?.name}</span>}
          />
        </section>
      )}
    </div>
  );
}

function SpaceTable({ spaces, third, cell }: { spaces: Space[]; third: string; cell: (space: Space) => ReactNode }) {
  return (
    <Table>
      <thead>
        <tr>
          <Th>{t.spaces.columnName}</Th>
          <Th>{t.spaces.columnKey}</Th>
          <Th>{third}</Th>
          <Th className="w-10">
            <span className="sr-only">{t.star.columnStar}</span>
          </Th>
        </tr>
      </thead>
      <tbody>
        {spaces.map((space) => (
          <tr key={space.id} data-space-row={space.key} data-archived={space.archivedAt ? "" : undefined}>
            <Td>
              <Link to="/s/$spaceKey" params={{ spaceKey: space.key }} className="font-medium text-ink hover:text-accent hover:underline">
                {space.name}
              </Link>
              {space.archivedAt && <ArchivedMark className="ml-2 align-middle" />}
            </Td>
            <Td>
              <span className="font-mono text-xs text-ink-muted">{space.key}</span>
            </Td>
            <Td>{cell(space)}</Td>
            <Td className="text-right">
              <SpaceStar space={space} size="sm" />
            </Td>
          </tr>
        ))}
      </tbody>
    </Table>
  );
}
