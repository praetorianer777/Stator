import { useState } from "react";
import { Link, useNavigate } from "@tanstack/react-router";
import { useSpaces } from "@/api/spaces";
import { ArchivedMark } from "@/features/archive/ArchiveBanner";
import { useCanCreateSpace } from "@/features/permissions/access";
import { SpaceStar } from "@/features/stars/StarButton";
import { Button, EmptyState, ErrorBanner, PageHeader, Skeleton, Switch, Table, Td, Th } from "@/components/ui";
import { Icon } from "@/components/icons";
import { t } from "@/i18n";

/** Every space the reader may see, archived ones when asked, and for administrators the way to make another. */
export function SpaceDirectory() {
  const [showArchived, setShowArchived] = useState(false);
  const { data: spaces, isLoading, error, refetch } = useSpaces(showArchived);
  const mayCreate = useCanCreateSpace();
  const navigate = useNavigate();
  const create = mayCreate ? (
    <Button icon={<Icon.Plus />} onClick={() => navigate({ to: "/spaces/new" })} data-action="new-space">
      {t.spaces.create}
    </Button>
  ) : undefined;
  const actions = (
    <>
      <span className="inline-flex items-center gap-2 text-sm text-ink-muted">
        <Switch label={t.archive.showArchived} checked={showArchived} onChange={setShowArchived} data-show-archived="" />
        <span aria-hidden="true">{t.archive.showArchived}</span>
      </span>
      {create}
    </>
  );

  return (
    <div className="mx-auto max-w-4xl" data-space-directory>
      <PageHeader title={t.spaces.title} actions={actions} />
      {error && <ErrorBanner onRetry={() => void refetch()}>{error.message}</ErrorBanner>}
      {isLoading && <Skeleton />}
      {spaces && spaces.length === 0 && (
        <EmptyState icon={<Icon.Space />} title={t.spaces.emptyTitle} description={mayCreate ? t.spaces.emptyBody : t.spaces.emptyBodyMember} action={create} />
      )}
      {spaces && spaces.length > 0 && (
        <Table>
          <thead>
            <tr>
              <Th>{t.spaces.columnName}</Th>
              <Th>{t.spaces.columnKey}</Th>
              <Th>{t.spaces.columnDescription}</Th>
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
                <Td>
                  <span className="text-ink-muted">{space.description}</span>
                </Td>
                <Td className="text-right">
                  <SpaceStar space={space} size="sm" />
                </Td>
              </tr>
            ))}
          </tbody>
        </Table>
      )}
    </div>
  );
}
