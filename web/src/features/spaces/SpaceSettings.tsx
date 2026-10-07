import { useState, type FormEvent } from "react";
import { Link, useNavigate } from "@tanstack/react-router";
import { ApiError } from "@/api/client";
import { useDeleteSpace, useSpace, useUpdateSpace, type Space } from "@/api/spaces";
import { Button, Card, ErrorBanner, Field, PageHeader, SectionTitle, Skeleton, TabPanel, Tabs } from "@/components/ui";
import { SPACE_DESCRIPTION_MAX_LENGTH, SPACE_NAME_MAX_LENGTH, STALE_PATH } from "@/config";
import { Icon } from "@/components/icons";
import { t } from "@/i18n";
import { SpacePermissions } from "@/features/permissions/SpacePermissions";
import { SpaceAnonymousAccess } from "@/features/public/AnonymousAccess";
import { ArchivePanel } from "@/features/archive/ArchivePanel";
import { SpaceArchive } from "@/features/archive/SpaceArchive";
import { TemplateList } from "@/features/templates/TemplateList";
import { ShortcutsPanel } from "@/features/shortcuts/ShortcutsPanel";
import { useCanAdministerOrg } from "@/features/permissions/access";
import { SpaceGuests } from "./SpaceGuests";
import { TrashPanel } from "./TrashPanel";

export type SettingsTab = "details" | "shortcuts" | "permissions" | "templates" | "guests" | "trash" | "archive";

const SETTINGS_PANEL_ID = "space-settings-panel";

/** A space's details, who may do what in it, and deleting it; changing anything is an administrator's. */
export function SpaceSettings({ spaceKey, tab, onTab }: { spaceKey: string; tab: SettingsTab; onTab: (tab: SettingsTab) => void }) {
  const { data: space, error, refetch } = useSpace(spaceKey);
  const orgAdmin = useCanAdministerOrg();
  if (error) return <ErrorBanner onRetry={() => void refetch()}>{error.message}</ErrorBanner>;
  if (!space) return <Skeleton />;
  // Guests are let into team spaces, by the organization's administrators.
  const guests = orgAdmin && !space.owner;
  const shown = tab === "guests" && !guests ? "details" : tab;
  return (
    <div className="mx-auto max-w-3xl" data-space-settings={space.key}>
      <PageHeader
        crumbs={[
          { label: t.spaces.title, render: (label) => <Link to="/spaces">{label}</Link> },
          {
            label: space.name,
            render: (label) => (
              <Link to="/s/$spaceKey" params={{ spaceKey: space.key }}>
                {label}
              </Link>
            ),
          },
        ]}
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
              { value: "templates", label: t.spaceSettings.templates, attrs: { "data-settings-tab": "templates" } },
              ...(guests ? [{ value: "guests" as const, label: t.spaceSettings.guests, attrs: { "data-settings-tab": "guests" } }] : []),
              { value: "trash", label: t.spaceSettings.trash, attrs: { "data-settings-tab": "trash" } },
              { value: "archive", label: t.spaceSettings.archive, attrs: { "data-settings-tab": "archive" } },
            ]}
          />
        }
      />
      <TabPanel id={SETTINGS_PANEL_ID} label={t.spaceSettings[shown]} className="space-y-6">
        {shown === "details" && <Details key={space.id} space={space} />}
        {shown === "shortcuts" && <ShortcutsPanel space={space} />}
        {shown === "permissions" && <SpacePermissions space={space} />}
        {shown === "permissions" && <SpaceAnonymousAccess space={space} />}
        {shown === "templates" && <TemplateList spaceKey={space.key} canEdit={space.can.administer} />}
        {shown === "guests" && <SpaceGuests space={space} />}
        {shown === "trash" && <TrashPanel space={space} />}
        {shown === "archive" && <ArchivePanel space={space} />}
      </TabPanel>
    </div>
  );
}

function Details({ space }: { space: Space }) {
  const update = useUpdateSpace(space.key);
  const [name, setName] = useState(space.name);
  const [description, setDescription] = useState(space.description);
  const [notice, setNotice] = useState("");
  const fields = update.error instanceof ApiError ? update.error.fields : {};
  const readOnly = !space.can.administer;

  function save(event: FormEvent) {
    event.preventDefault();
    setNotice("");
    update.mutate({ name, description }, { onSuccess: () => setNotice(t.spaceSettings.saved) });
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
            <span role="status" className="text-sm text-ink-muted">
              {notice}
            </span>
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
  return (
    <Card className="space-y-3 border-danger/40 p-4" data-space-danger>
      <SectionTitle>{t.spaceSettings.dangerTitle}</SectionTitle>
      <p className="text-sm text-ink-muted">{t.spaceSettings.dangerBody}</p>
      {remove.error && <ErrorBanner>{remove.error.message}</ErrorBanner>}
      <Button
        variant="danger"
        loading={remove.isPending}
        onClick={() => {
          if (window.confirm(t.spaceSettings.confirmDelete(space.name, space.key))) {
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
