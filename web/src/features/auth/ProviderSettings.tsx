import { useState, type FormEvent } from "react";
import { ApiError } from "@/api/client";
import { ssoStartURL, useMe, useProvider, useSaveProvider, type Provider } from "@/api/auth";
import { Button, Card, Checkbox, ErrorBanner, Field, Skeleton } from "@/components/ui";
import { SSO_DEFAULT_GROUPS_CLAIM, SSO_DEFAULT_SCOPES } from "@/config";
import { t } from "@/i18n";

interface Draft {
  issuer: string;
  clientId: string;
  clientSecret: string;
  groupsClaim: string;
  scopes: string;
  createGroups: boolean;
  enabled: boolean;
}

function draftOf(provider: Provider | null | undefined): Draft {
  return {
    issuer: provider?.issuer ?? "",
    clientId: provider?.clientId ?? "",
    clientSecret: "",
    groupsClaim: provider?.groupsClaim ?? SSO_DEFAULT_GROUPS_CLAIM,
    scopes: provider?.scopes ?? SSO_DEFAULT_SCOPES,
    createGroups: provider?.createGroups ?? false,
    enabled: provider?.enabled ?? true,
  };
}

/** The organization's identity provider, for its administrators. */
export function ProviderSettings() {
  const { data, isLoading, error } = useProvider();
  if (isLoading) return <Skeleton />;
  if (error) return <ErrorBanner>{error instanceof ApiError && error.status === 403 ? t.sso.notAdmin : error.message}</ErrorBanner>;
  // Keyed by the saved row, so a save that comes back starts the form afresh.
  return <ProviderForm key={data?.provider?.updatedAt ?? "new"} provider={data?.provider ?? null} callbackUrl={data?.callbackUrl ?? ""} />;
}

// The client secret is write-only: it never comes back, and a blank field
// keeps the stored one, so saving the rest of the form cannot erase it.
function ProviderForm({ provider, callbackUrl }: { provider: Provider | null; callbackUrl: string }) {
  const save = useSaveProvider();
  const { data: me } = useMe();
  const [draft, setDraft] = useState(() => draftOf(provider));
  const [saved, setSaved] = useState(false);
  const fields = save.error instanceof ApiError ? save.error.fields : {};
  const formError = save.error && Object.keys(fields).length === 0 ? save.error.message : null;
  const slug = me?.organization?.slug ?? "";

  function set<K extends keyof Draft>(key: K, value: Draft[K]) {
    setDraft((current) => ({ ...current, [key]: value }));
    setSaved(false);
  }

  function submit(event: FormEvent) {
    event.preventDefault();
    const { clientSecret, ...rest } = draft;
    save.mutate(clientSecret ? { ...rest, clientSecret } : rest, { onSuccess: () => setSaved(true) });
  }

  return (
    <form onSubmit={submit} noValidate aria-label={t.sso.title}>
      <Card className="space-y-4 p-4">
        <p className="text-sm text-ink-muted">{t.sso.intro}</p>
        <div className="grid gap-3 sm:grid-cols-2">
          <Field
            label={t.sso.issuer}
            value={draft.issuer}
            placeholder="https://id.example.com/realms/acme"
            hint={t.sso.issuerHint}
            error={fields.issuer}
            onChange={(event) => set("issuer", event.target.value)}
          />
          <Field label={t.sso.clientId} value={draft.clientId} error={fields.clientId} onChange={(event) => set("clientId", event.target.value)} />
          <Field
            label={t.sso.clientSecret}
            type="password"
            autoComplete="off"
            value={draft.clientSecret}
            placeholder={provider?.hasSecret ? t.sso.clientSecretStored : ""}
            error={fields.clientSecret}
            onChange={(event) => set("clientSecret", event.target.value)}
          />
          <Field
            label={t.sso.groupsClaim}
            value={draft.groupsClaim}
            hint={t.sso.groupsClaimHint}
            onChange={(event) => set("groupsClaim", event.target.value)}
          />
          <Field label={t.sso.scopes} value={draft.scopes} onChange={(event) => set("scopes", event.target.value)} />
        </div>
        <div className="flex flex-col gap-2">
          <Checkbox label={t.sso.enabled} checked={draft.enabled} onChange={(event) => set("enabled", event.target.checked)} />
          <Checkbox label={t.sso.createGroups} checked={draft.createGroups} onChange={(event) => set("createGroups", event.target.checked)} />
        </div>
        {callbackUrl && (
          <p className="text-sm text-ink-muted">
            {t.sso.callback} <code className="rounded bg-surface-raised px-1 font-mono text-xs text-ink">{callbackUrl}</code>
          </p>
        )}
        {provider?.enabled && slug && (
          <p className="text-sm text-ink-muted">
            {t.sso.signInAt} <code className="rounded bg-surface-raised px-1 font-mono text-xs text-ink">{ssoStartURL(slug)}</code>
          </p>
        )}
        {formError && <ErrorBanner>{formError}</ErrorBanner>}
        <div className="flex items-center gap-3">
          <Button type="submit" loading={save.isPending} data-action="save-sso">
            {provider ? t.sso.save : t.sso.setUp}
          </Button>
          {saved && (
            <span role="status" className="text-sm text-ink-muted">
              {t.sso.saved}
            </span>
          )}
        </div>
      </Card>
    </form>
  );
}
