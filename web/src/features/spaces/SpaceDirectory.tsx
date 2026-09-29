import { Link, useNavigate } from "@tanstack/react-router";
import { useSpaces } from "@/api/spaces";
import { useViewer } from "@/api/viewer";
import { Button, EmptyState, ErrorBanner, PageHeader, Skeleton, Table, Td, Th } from "@/components/ui";
import { Icon } from "@/components/icons";
import { t } from "@/i18n";

/** Every space the reader may see, and for administrators the way to make another. */
export function SpaceDirectory() {
  const { data: spaces, isLoading, error, refetch } = useSpaces();
  const { administers } = useViewer();
  const navigate = useNavigate();
  const create = administers ? (
    <Button icon={<Icon.Plus />} onClick={() => navigate({ to: "/spaces/new" })} data-action="new-space">
      {t.spaces.create}
    </Button>
  ) : undefined;

  return (
    <div className="mx-auto max-w-4xl" data-space-directory>
      <PageHeader title={t.spaces.title} actions={create} />
      {error && <ErrorBanner onRetry={() => void refetch()}>{error.message}</ErrorBanner>}
      {isLoading && <Skeleton />}
      {spaces && spaces.length === 0 && (
        <EmptyState
          icon={<Icon.Space />}
          title={t.spaces.emptyTitle}
          description={administers ? t.spaces.emptyBody : t.spaces.emptyBodyMember}
          action={create}
        />
      )}
      {spaces && spaces.length > 0 && (
        <Table>
          <thead>
            <tr>
              <Th>{t.spaces.columnName}</Th>
              <Th>{t.spaces.columnKey}</Th>
              <Th>{t.spaces.columnDescription}</Th>
            </tr>
          </thead>
          <tbody>
            {spaces.map((space) => (
              <tr key={space.id} data-space-row={space.key}>
                <Td>
                  <Link to="/s/$spaceKey" params={{ spaceKey: space.key }} className="font-medium text-ink hover:text-accent hover:underline">
                    {space.name}
                  </Link>
                </Td>
                <Td>
                  <span className="font-mono text-xs text-ink-muted">{space.key}</span>
                </Td>
                <Td>
                  <span className="text-ink-muted">{space.description}</span>
                </Td>
              </tr>
            ))}
          </tbody>
        </Table>
      )}
    </div>
  );
}
