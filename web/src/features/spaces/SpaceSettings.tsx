import { useState, type FormEvent } from "react";
import { Link, useNavigate } from "@tanstack/react-router";
import { ApiError } from "@/api/client";
import { useDeleteSpace, useSpace, useUpdateSpace, type Space } from "@/api/spaces";
import { TabPanel, Tabs, Page, PageHeader, Button, Card, Field, ErrorBanner, SectionTitle, Skeleton, useToast } from "@/components/ui";
import { SPACE_DESCRIPTION_MAX_LENGTH, SPACE_NAME_MAX_LENGTH, STALE_PATH } from "@/config";
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

export type SettingsTab = "details" | "shortcuts" | "permissions" | "reviews" | "templates" | "guests" | "trash" | "archive" | "export";

const SETTINGS_PANEL_ID = "space-settings-panel";

/** A space's details, who may do what in it, and deleting it; changing anything is an administrator's. */
export function SpaceSettings({ spaceKey, tab, onTab }: { spaceKey: string; tab: SettingsTab; onTab: (tab: SettingsTab) => void }) {
  const { data: space, error, refetch } = useSpace(spaceKey);
  const orgAdmin = useCanAdministerSite();
  if (error) return <ErrorBanner onRetry={() => void refetch()}>{error.message}</ErrorBanner>;
  if (!space) return <Skeleton />;
  // Guests are let into team spaces, by the organization's administrators.
  const guests = orgAdmin && !space.owner;
  const shown = (tab === "guests" && !guests) || ((tab === "export" || tab === "reviews") && !space.can.administer) ? "details" : tab;
  return (
    <Page width="narrow" data-space-settings={space.key}>
      <PageHeader
        title={t.spaceSettings.title}
        actions={
          space.can.administer && (
            <Link
              to={STALE_PATH}
              search={{ space: space.key }}
              className="inline-flex h-8 items-center gap-1.5 rounded-control border border-border-strong bg-surface px-3 text-sm font-medium text-ink no-underline hover:bg-surface-raised"
              data-action="space-stale"
            >
              <Icon.Calendar />
              {t.spaceSettings.stale}
            </Link>
          )
        }
        tabs={
          <Tabs<SettingsTab>
            label={t.spaceSettings.tabs}
            value={shown}
            onChange={onTab}
            panelId={SETTINGS_PANEL_ID}
            tabs={[
              { value: "details", label: t.spaceSettings.details, attrs: { "data-settings-tab": "details" } },
              { value: "shortcuts", label: t.spaceSettings.shortcuts, attrs: { "data-settings-tab": "shortcuts" } },
              { value: "permissions", label: t.spaceSettings.permissions, attrs: { "data-settings-tab": "permissions" } },
              ...(space.can.administer ? [{ value: "reviews" as const, label: t.spaceSettings.reviews, attrs: { "data-settings-tab": "reviews" } }] : []),
              { value: "templates", label: t.spaceSettings.templates, attrs: { "data-settings-tab": "templates" } },
              ...(guests ? [{ value: "guests" as const, label: t.spaceSettings.guests, attrs: { "data-settings-tab": "guests" } }] : []),
              { value: "trash", label: t.spaceSettings.trash, attrs: { "data-settings-tab": "trash" } },
              { value: "archive", label: t.spaceSettings.archive, attrs: { "data-settings-tab": "archive" } },
              ...(space.can.administer ? [{ value: "export" as const, label: t.spaceSettings.export, attrs: { "data-settings-tab": "export" } }] : []),
            ]}
          />
        }
      />
      <TabPanel id={SETTINGS_PANEL_ID} label={t.spaceSettings[shown]} className="space-y-6">
        {shown === "details" && <Details key={space.id} space={space} />}
        {shown === "shortcuts" && <ShortcutsPanel space={space} />}
        {shown === "permissions" && <SpacePermissions space={space} />}
        {shown === "permissions" && <SpaceAnonymousAccess space={space} />}
        {shown === "reviews" && <SpaceReviews space={space} />}
        {shown === "templates" && <TemplateList spaceKey={space.key} canEdit={space.can.administer} />}
        {shown === "guests" && <SpaceGuests space={space} />}
        {shown === "trash" && <TrashPanel space={space} />}
        {shown === "archive" && <ArchivePanel space={space} />}
        {shown === "export" && <SpaceExportPanel space={space} />}
      </TabPanel>
    </Page>
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
    <>
      {readOnly && <p className="text-sm text-ink-muted">{t.spaceSettings.notAdmin}</p>}
      <form onSubmit={save} className="space-y-4" noValidate data-space-details>
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
      {space.can.administer && <SpaceArchive space={space} />}
      {space.can.delete && <DeleteSpace space={space} />}
    </>
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
