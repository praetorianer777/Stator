import { useId, useState, type FormEvent, type ReactNode } from "react";
import { Link, useNavigate } from "@tanstack/react-router";
import { ApiError } from "@/api/client";
import { useDeleteSpace, useSpace, useUpdateSpace, type Space } from "@/api/spaces";
import { TabPanel, Tabs, Page, PageHeader, Button, Card, EmptyState, Field, ErrorBanner, SectionTitle, Skeleton, Tag, useToast } from "@/components/ui";
import { SPACE_DESCRIPTION_MAX_LENGTH, SPACE_NAME_MAX_LENGTH, SPACE_STALE_PATH } from "@/config";
import { Icon } from "@/components/icons";
import { t } from "@/i18n";
import { SpacePermissions } from "@/features/permissions/SpacePermissions";
import { SpaceAnonymousAccess } from "@/features/public/AnonymousAccess";
import { ArchivePanel } from "@/features/archive/ArchivePanel";
import { SpaceArchive } from "@/features/archive/SpaceArchive";
import { TemplateList } from "@/features/templates/TemplateList";
import { ShortcutsPanel } from "@/features/shortcuts/ShortcutsPanel";
import { useCanAdministerSite } from "@/features/permissions/access";
import { SpaceExportPanel } from "./SpaceExport";
import { SpaceGuests } from "./SpaceGuests";
import { SpaceReviews } from "@/features/reviews/SpaceReviews";
import { TrashPanel } from "./TrashPanel";
import { useConfirm } from "@/features/shell/ConfirmProvider";
import { ARCHIVE_ANCHOR, GUESTS_ANCHOR, adminOnly, sectionLabel, sectionsFor, spaceSettingsPath, type SpaceSettingsSection } from "./settingsSections";

const linkButton =
  "inline-flex h-8 items-center gap-1.5 rounded-control border border-border-strong bg-surface px-3 text-sm font-medium text-ink no-underline hover:bg-surface-raised";

/**
 * A space's settings: the tabs of its sections in the header and the one
 * section shown. Every member opens them to see how the space is set up; what
 * only its administrators use is neither listed nor mounted for anybody else.
 */
export function SpaceSettingsLayout({ spaceKey, section, children }: { spaceKey: string; section: SpaceSettingsSection; children: ReactNode }) {
  const { data: space, error, refetch } = useSpace(spaceKey);
  const navigate = useNavigate();
  const panelId = useId();
  if (error) return <ErrorBanner onRetry={() => void refetch()}>{error.message}</ErrorBanner>;
  if (!space) {
    return (
      <Page width="content">
        <Skeleton lines={6} />
      </Page>
    );
  }
  const administers = space.can.administer;
  const refused = adminOnly(section) && !administers;
  return (
    <Page width="content" data-space-settings={space.key}>
      <PageHeader
        title={t.spaceSettings.title}
        meta={<Tag className="font-mono">{space.key}</Tag>}
        tabs={
          <Tabs<SpaceSettingsSection>
            label={t.spaceSettings.tabs}
            value={section}
            onChange={(next) => void navigate({ href: spaceSettingsPath(space.key, next) })}
            panelId={panelId}
            tabs={sectionsFor(space).map((each) => ({ value: each, label: sectionLabel(each), attrs: { "data-settings-tab": each } }))}
          />
        }
      />
      {!administers && !refused && (
        <p role="status" className="mb-4 rounded-control border border-border bg-surface-raised px-3 py-2 text-sm text-ink-muted" data-settings-read-only>
          {t.spaceSettings.readOnly}
        </p>
      )}
      <TabPanel id={panelId} label={sectionLabel(section)}>
        {refused ? (
          <div data-settings-refused={section}>
            <EmptyState icon={<Icon.Lock />} title={t.spaceSettings.notAdmin} description={t.spaceSettings.notAdminDetail} />
          </div>
        ) : (
          <div className="space-y-6" data-settings-section={section}>
            {children}
          </div>
        )}
      </TabPanel>
    </Page>
  );
}

/** A section's body once its space is known; the layout above waits for it and says when it cannot be had. */
function WithSpace({ spaceKey, children }: { spaceKey: string; children: (space: Space) => ReactNode }) {
  const { data: space } = useSpace(spaceKey);
  return space ? <>{children(space)}</> : null;
}

/** The space's details, a way to its stale pages, and at the foot archiving and deleting it. */
export function GeneralSection({ spaceKey }: { spaceKey: string }) {
  return (
    <WithSpace spaceKey={spaceKey}>
      {(space) => (
        <>
          <Details key={space.id} space={space} />
          {space.can.administer && <StaleLink space={space} />}
          {space.can.administer && <SpaceArchive space={space} />}
          {space.can.delete && <DeleteSpace space={space} />}
        </>
      )}
    </WithSpace>
  );
}

export function WorkflowSection({ spaceKey }: { spaceKey: string }) {
  return <WithSpace spaceKey={spaceKey}>{(space) => <SpaceReviews space={space} />}</WithSpace>;
}

export function TemplatesSection({ spaceKey }: { spaceKey: string }) {
  return <WithSpace spaceKey={spaceKey}>{(space) => <TemplateList spaceKey={space.key} canEdit={space.can.administer} />}</WithSpace>;
}

/** Who holds which role, whether anybody may read without signing in, and the guests of a team space. */
export function MembersSection({ spaceKey }: { spaceKey: string }) {
  const siteAdmin = useCanAdministerSite();
  return (
    <WithSpace spaceKey={spaceKey}>
      {(space) => (
        <>
          <SpacePermissions space={space} />
          <SpaceAnonymousAccess space={space} />
          {/* Guests are let into team spaces, by the installation's administrators. */}
          {siteAdmin && !space.owner && (
            <div id={GUESTS_ANCHOR} className="scroll-mt-24">
              <SpaceGuests space={space} />
            </div>
          )}
        </>
      )}
    </WithSpace>
  );
}

export function IntegrationsSection({ spaceKey }: { spaceKey: string }) {
  return <WithSpace spaceKey={spaceKey}>{(space) => <ShortcutsPanel space={space} />}</WithSpace>;
}

export function ExportSection({ spaceKey }: { spaceKey: string }) {
  return <WithSpace spaceKey={spaceKey}>{(space) => <SpaceExportPanel space={space} />}</WithSpace>;
}

export function TrashSection({ spaceKey }: { spaceKey: string }) {
  return (
    <WithSpace spaceKey={spaceKey}>
      {(space) => (
        <>
          <section aria-labelledby="space-trash-title" className="space-y-3">
            <SectionTitle id="space-trash-title">{t.spaceSettings.trash}</SectionTitle>
            <TrashPanel space={space} />
          </section>
          <section id={ARCHIVE_ANCHOR} aria-labelledby="space-archive-title" className="scroll-mt-24 space-y-3">
            <SectionTitle id="space-archive-title">{t.spaceSettings.archive}</SectionTitle>
            <ArchivePanel space={space} />
          </section>
        </>
      )}
    </WithSpace>
  );
}

function Details({ space }: { space: Space }) {
  const update = useUpdateSpace(space.key);
  const [name, setName] = useState(space.name);
  const [description, setDescription] = useState(space.description);
  const toast = useToast();
  const fields = update.error instanceof ApiError ? update.error.fields : {};
  const readOnly = !space.can.administer;

  function save(event: FormEvent) {
    event.preventDefault();
    update.mutate({ name, description }, { onSuccess: () => toast.success(t.spaceSettings.saved) });
  }

  return (
    <Card className="p-4">
      <form onSubmit={save} className="space-y-4" noValidate data-space-details>
        <SectionTitle>{t.spaceSettings.details}</SectionTitle>
        {update.error && !Object.keys(fields).length && <ErrorBanner>{update.error.message}</ErrorBanner>}
        <Field label={t.spaces.key} value={space.key} readOnly className="font-mono" hint={t.spaces.keyHint} />
        <Field
          label={t.spaces.name}
          value={name}
          maxLength={SPACE_NAME_MAX_LENGTH}
          onChange={(event) => setName(event.target.value)}
          readOnly={readOnly}
          error={fields.name}
        />
        <Field
          label={t.spaces.description}
          value={description}
          rows={3}
          maxLength={SPACE_DESCRIPTION_MAX_LENGTH}
          onChange={(event) => setDescription(event.target.value)}
          readOnly={readOnly}
          error={fields.description}
        />
        {!readOnly && (
          <div className="flex items-center gap-3">
            <Button type="submit" loading={update.isPending} data-action="save-space">
              {t.spaceSettings.save}
            </Button>
          </div>
        )}
      </form>
    </Card>
  );
}

function StaleLink({ space }: { space: Space }) {
  return (
    <Card className="flex flex-wrap items-center justify-between gap-3 p-4" data-space-stale-card>
      <div className="space-y-1">
        <SectionTitle>{t.spaceSettings.stale}</SectionTitle>
        <p className="text-sm text-ink-muted">{t.spaceSettings.staleIntro}</p>
      </div>
      <Link to={SPACE_STALE_PATH} params={{ spaceKey: space.key }} className={linkButton} data-action="space-stale">
        <Icon.Calendar />
        {t.spaceSettings.staleOpen}
      </Link>
    </Card>
  );
}

function DeleteSpace({ space }: { space: Space }) {
  const remove = useDeleteSpace(space.key);
  const navigate = useNavigate();
  const confirm = useConfirm();
  return (
    <Card className="space-y-3 border-danger/40 p-4" data-space-danger>
      <SectionTitle>{t.spaceSettings.dangerTitle}</SectionTitle>
      <p className="text-sm text-ink-muted">{t.spaceSettings.dangerBody}</p>
      {remove.error && <ErrorBanner>{remove.error.message}</ErrorBanner>}
      <Button
        variant="danger"
        loading={remove.isPending}
        onClick={async () => {
          if (await confirm({ noun: t.kit.confirm.things.space(space.name), body: t.spaceSettings.confirmDelete })) {
            remove.mutate(undefined, { onSuccess: () => void navigate({ to: "/spaces" }) });
          }
        }}
        data-action="delete-space"
      >
        {t.spaceSettings.delete}
      </Button>
    </Card>
  );
}
