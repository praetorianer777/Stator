import { useState, type FormEvent } from "react";
import { Link, useNavigate } from "@tanstack/react-router";
import { ApiError } from "@/api/client";
import { useDeleteSpace, useSpace, useUpdateSpace, type Space } from "@/api/spaces";
import { Button, Card, ErrorBanner, Field, PageHeader, SectionTitle, Skeleton, TabPanel, Tabs } from "@/components/ui";
import { SPACE_DESCRIPTION_MAX_LENGTH, SPACE_NAME_MAX_LENGTH } from "@/config";
import { t } from "@/i18n";
import { TrashPanel } from "./TrashPanel";

export type SettingsTab = "details" | "permissions" | "trash";

const SETTINGS_PANEL_ID = "space-settings-panel";

/** A space's details, who may do what in it, and deleting it; changing anything is an administrator's. */
export function SpaceSettings({ spaceKey, tab, onTab }: { spaceKey: string; tab: SettingsTab; onTab: (tab: SettingsTab) => void }) {
  const { data: space, error, refetch } = useSpace(spaceKey);
  if (error) return <ErrorBanner onRetry={() => void refetch()}>{error.message}</ErrorBanner>;
  if (!space) return <Skeleton />;
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
        tabs={
          <Tabs<SettingsTab>
            label={t.spaceSettings.tabs}
            value={tab}
            onChange={onTab}
            panelId={SETTINGS_PANEL_ID}
            tabs={[
              { value: "details", label: t.spaceSettings.details, attrs: { "data-settings-tab": "details" } },
              { value: "permissions", label: t.spaceSettings.permissions, attrs: { "data-settings-tab": "permissions" } },
              { value: "trash", label: t.spaceSettings.trash, attrs: { "data-settings-tab": "trash" } },
            ]}
          />
        }
      />
      <TabPanel id={SETTINGS_PANEL_ID} label={t.spaceSettings[tab]} className="space-y-6">
        {tab === "details" && <Details key={space.id} space={space} />}
        {tab === "permissions" && <Permissions />}
        {tab === "trash" && <TrashPanel space={space} />}
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

function Permissions() {
  return (
    <div className="space-y-2 text-sm text-ink-muted" data-space-permissions>
      <p>{t.spaceSettings.permissionsIntro}</p>
      <p>{t.spaceSettings.permissionsLater}</p>
    </div>
  );
}
