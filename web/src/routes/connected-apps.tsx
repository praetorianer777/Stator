import { useState, type FormEvent } from "react";
import { createRoute } from "@tanstack/react-router";
import { ApiError } from "@/api/client";
import {
  useConnectedApps,
  useCreateConnectedApp,
  useDeleteConnectedApp,
  useInstallationTokens,
  useRevokeInstallationToken,
  useUpdateConnectedApp,
  type ConnectedApp,
  type ConnectedAppInput,
} from "@/api/oauth";
import { Button, Card, Checkbox, EmptyState, ErrorBanner, Field, Page, PageHeader, Skeleton, Table, Tag, Td, Th, useToast } from "@/components/ui";
import { Icon } from "@/components/icons";
import { OAUTH_SCOPE_READ, OAUTH_SCOPE_WRITE } from "@/config";
import { useConfirm } from "@/features/shell/ConfirmProvider";
import { t } from "@/i18n";
import { localDateFormat } from "@/lib/format";
import { adminRoute } from "./admin";

// Adapted from Armature's Connected apps page.

const when = localDateFormat({ dateStyle: "medium", timeStyle: "short" });

/** The apps that may act for people here, and every token of the installation. */
export const connectedAppsRoute = createRoute({
  getParentRoute: () => adminRoute,
  path: "/connected-apps",
  component: ConnectedAppsPage,
});

function ConnectedAppsPage() {
  const a = t.admin.apps;
  const { data, isLoading, error } = useConnectedApps();
  const create = useCreateConnectedApp();
  const remove = useDeleteConnectedApp();
  const confirm = useConfirm();
  const toast = useToast();
  const [adding, setAdding] = useState(false);
  const [editing, setEditing] = useState<ConnectedApp | null>(null);
  // The secret is shown once, right after the app is added, and never again.
  const [fresh, setFresh] = useState<ConnectedApp | null>(null);
  const apps = data ?? [];

  async function removeApp(app: ConnectedApp) {
    const things = t.kit.confirm.things;
    if (!(await confirm({ noun: things.named(things.app, app.name), verb: t.kit.confirm.verbs.remove, body: a.removeBody(app.name) }))) return;
    remove.mutate(app.id, { onSuccess: () => toast.success(a.removed(app.name)) });
  }

  return (
    <Page width="content" data-connected-apps-page="">
      <PageHeader
        title={a.title}
        actions={
          !adding && (
            <Button icon={<Icon.Plus />} onClick={() => setAdding(true)} data-action="new-connected-app">
              {a.add}
            </Button>
          )
        }
      />
      <p className="mb-4 text-sm text-ink-muted">{a.intro}</p>

      {fresh?.secret && (
        <Card className="mb-4 space-y-2 p-4" data-connected-app-secret="">
          <p className="text-sm font-medium text-ink">{a.secretFor(fresh.name)}</p>
          <p className="text-sm text-ink-muted">{a.secretOnce}</p>
          <dl className="grid grid-cols-[auto_1fr] items-center gap-x-3 gap-y-1 text-sm">
            <dt className="text-ink-muted">{a.clientId}</dt>
            <dd>
              <code className="rounded-control bg-surface-raised px-2 py-1 font-mono break-all text-ink" data-client-id>
                {fresh.id}
              </code>
            </dd>
            <dt className="text-ink-muted">{a.secret}</dt>
            <dd>
              <code className="rounded-control bg-surface-raised px-2 py-1 font-mono break-all text-ink" data-client-secret>
                {fresh.secret}
              </code>
            </dd>
          </dl>
          <Button variant="secondary" size="sm" onClick={() => setFresh(null)}>
            {a.done}
          </Button>
        </Card>
      )}

      {adding && (
        <Card className="mb-4 p-4">
          <AppForm
            title={a.addTitle}
            submit={a.create}
            pending={create.isPending}
            error={create.error}
            onCancel={() => setAdding(false)}
            onSubmit={(input) =>
              create.mutate(input, {
                onSuccess: (app) => {
                  setAdding(false);
                  setFresh(app);
                  toast.success(a.added(app.name));
                },
              })
            }
          />
        </Card>
      )}

      {error && <ErrorBanner>{error.message}</ErrorBanner>}
      {remove.error && <ErrorBanner>{remove.error.message}</ErrorBanner>}
      {isLoading ? (
        <Skeleton lines={3} />
      ) : apps.length === 0 ? (
        <EmptyState icon={<Icon.Key />} title={a.empty} description={a.emptyBody} />
      ) : (
        <Table aria-label={a.title}>
          <thead>
            <tr>
              <Th>{a.columnApp}</Th>
              <Th className="hidden sm:table-cell">{a.columnScopes}</Th>
              <Th>{a.columnPeople}</Th>
              <Th>
                <span className="sr-only">{a.edit}</span>
              </Th>
            </tr>
          </thead>
          <tbody>
            {apps.map((app) => (
              <tr key={app.id} data-connected-app={app.name} data-connected-app-status={app.status}>
                <Td className="max-w-0 min-w-0">
                  <span className="flex flex-wrap items-center gap-2 font-medium text-ink">
                    {app.name}
                    <Tag>{app.kind === "partner" ? a.partner : a.registered}</Tag>
                    {app.status === "pending" && <Tag>{a.pending}</Tag>}
                  </span>
                  <span className="block truncate text-xs text-ink-muted">{app.keycloakClientId || app.clientUri || app.redirectUris[0]}</span>
                </Td>
                <Td className="hidden text-sm text-ink-muted sm:table-cell">{app.scopes.join(", ")}</Td>
                <Td className="text-sm whitespace-nowrap text-ink-muted">{a.people(app.grants)}</Td>
                <Td>
                  <div className="flex justify-end gap-1">
                    {app.kind === "partner" && (
                      <Button size="sm" variant="secondary" onClick={() => setEditing(app)} aria-label={a.editWho(app.name)} data-action="connected-app-edit">
                        {a.edit}
                      </Button>
                    )}
                    <Button
                      size="sm"
                      variant="secondary"
                      onClick={() => void removeApp(app)}
                      aria-label={a.removeWho(app.name)}
                      data-action="connected-app-remove"
                    >
                      {a.remove}
                    </Button>
                  </div>
                </Td>
              </tr>
            ))}
          </tbody>
        </Table>
      )}
      {editing && <EditApp key={editing.id} app={editing} onClose={() => setEditing(null)} />}

      <InstallationTokens />
    </Page>
  );
}

function EditApp({ app, onClose }: { app: ConnectedApp; onClose: () => void }) {
  const update = useUpdateConnectedApp();
  const toast = useToast();
  return (
    <Card className="mt-4 p-4" data-connected-app-editing={app.name}>
      <AppForm
        title={t.admin.apps.editWho(app.name)}
        submit={t.admin.apps.save}
        initial={app}
        pending={update.isPending}
        error={update.error}
        onCancel={onClose}
        onSubmit={(changes) =>
          update.mutate(
            { id: app.id, changes },
            {
              onSuccess: (saved) => {
                toast.success(t.admin.apps.saved(saved.name));
                onClose();
              },
            },
          )
        }
      />
    </Card>
  );
}

function AppForm({
  title,
  submit,
  initial,
  pending,
  error,
  onCancel,
  onSubmit,
}: {
  title: string;
  submit: string;
  initial?: ConnectedApp;
  pending: boolean;
  error: Error | null;
  onCancel: () => void;
  onSubmit: (input: ConnectedAppInput) => void;
}) {
  const a = t.admin.apps;
  const [name, setName] = useState(initial?.name ?? "");
  const [keycloakClientId, setKeycloakClientId] = useState(initial?.keycloakClientId ?? "");
  const [scopes, setScopes] = useState<string[]>(initial?.scopes ?? [OAUTH_SCOPE_READ]);
  const [noScope, setNoScope] = useState(false);
  const fields = error instanceof ApiError ? error.fields : {};
  const formError = error && Object.keys(fields).length === 0 ? error.message : null;

  function toggle(scope: string, on: boolean) {
    setNoScope(false);
    setScopes((current) => (on ? [...current.filter((each) => each !== scope), scope] : current.filter((each) => each !== scope)));
  }

  function send(event: FormEvent) {
    event.preventDefault();
    if (scopes.length === 0) {
      setNoScope(true);
      return;
    }
    onSubmit({ name: name.trim(), keycloakClientId: keycloakClientId.trim(), scopes });
  }

  return (
    <form onSubmit={send} noValidate aria-label={title} className="space-y-3" data-connected-app-form="">
      <h2 className="text-sm font-semibold text-ink">{title}</h2>
      <div className="grid gap-3 sm:grid-cols-2">
        <Field label={a.name} value={name} error={fields.name} onChange={(event) => setName(event.target.value)} data-connected-app-name />
        <Field
          label={a.keycloakClientId}
          value={keycloakClientId}
          hint={a.keycloakClientIdHint}
          error={fields.keycloakClientId}
          onChange={(event) => setKeycloakClientId(event.target.value)}
          data-connected-app-keycloak
        />
      </div>
      <fieldset className="space-y-1">
        <legend className="text-sm font-medium text-ink">{a.scopes}</legend>
        <div className="flex flex-wrap gap-4">
          <Checkbox label={a.scopeRead} checked={scopes.includes(OAUTH_SCOPE_READ)} onChange={(event) => toggle(OAUTH_SCOPE_READ, event.target.checked)} />
          <Checkbox label={a.scopeWrite} checked={scopes.includes(OAUTH_SCOPE_WRITE)} onChange={(event) => toggle(OAUTH_SCOPE_WRITE, event.target.checked)} />
        </div>
        {(noScope || fields.scopes) && <p className="text-sm text-danger">{fields.scopes ?? a.scopesMissing}</p>}
      </fieldset>
      {formError && <ErrorBanner>{formError}</ErrorBanner>}
      <div className="flex gap-2">
        <Button type="submit" loading={pending} data-action="save-connected-app">
          {submit}
        </Button>
        <Button type="button" variant="secondary" onClick={onCancel}>
          {a.cancel}
        </Button>
      </div>
    </form>
  );
}

/** Everybody's tokens, so an administrator can stop one that leaked or an app that should not hold one. */
function InstallationTokens() {
  const a = t.admin.apps;
  const { data, isLoading, error } = useInstallationTokens();
  const revoke = useRevokeInstallationToken();
  const confirm = useConfirm();
  const toast = useToast();
  const tokens = data ?? [];

  async function revokeToken(id: string, name: string) {
    const things = t.kit.confirm.things;
    if (!(await confirm({ noun: things.named(things.token, name), verb: t.kit.confirm.verbs.revoke, body: a.revokeBody }))) return;
    revoke.mutate(id, { onSuccess: () => toast.success(a.revoked(name)) });
  }

  return (
    <section aria-labelledby="installation-tokens" className="mt-8 space-y-2" data-installation-tokens="">
      <h2 id="installation-tokens" className="text-base font-semibold text-ink">
        {a.tokensTitle}
      </h2>
      <p className="text-sm text-ink-muted">{a.tokensIntro}</p>
      {error && <ErrorBanner>{error.message}</ErrorBanner>}
      {revoke.error && <ErrorBanner>{revoke.error.message}</ErrorBanner>}
      {isLoading ? (
        <Skeleton lines={3} />
      ) : tokens.length === 0 ? (
        <p className="text-sm text-ink-subtle">{a.tokensEmpty}</p>
      ) : (
        <Table dense aria-label={a.tokensTitle}>
          <thead>
            <tr>
              <Th>{a.columnToken}</Th>
              <Th>{a.columnOwner}</Th>
              <Th className="hidden sm:table-cell">{a.columnLastUsed}</Th>
              <Th>
                <span className="sr-only">{a.revoke}</span>
              </Th>
            </tr>
          </thead>
          <tbody>
            {tokens.map((token) => {
              const owner = token.owner.name || token.owner.email;
              return (
                <tr key={token.id} data-installation-token={token.name}>
                  <Td className="max-w-0 min-w-0">
                    <span className="flex flex-wrap items-center gap-2 text-ink">
                      <span className="truncate">{token.name}</span>
                      {token.clientId && <Tag>{a.viaApp}</Tag>}
                    </span>
                  </Td>
                  <Td className="text-sm text-ink-muted">{owner}</Td>
                  <Td className="hidden text-sm whitespace-nowrap text-ink-muted sm:table-cell">
                    {token.lastUsedAt ? when.format(new Date(token.lastUsedAt)) : a.neverUsed}
                  </Td>
                  <Td className="text-right">
                    <Button
                      size="sm"
                      variant="secondary"
                      onClick={() => void revokeToken(token.id, token.name)}
                      aria-label={a.revokeWho(token.name, owner)}
                      data-action="revoke-installation-token"
                    >
                      {a.revoke}
                    </Button>
                  </Td>
                </tr>
              );
            })}
          </tbody>
        </Table>
      )}
    </section>
  );
}
