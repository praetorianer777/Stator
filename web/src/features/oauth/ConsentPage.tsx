import { useEffect, useState } from "react";
import { ApiError } from "@/api/client";
import { useConsent, useDecideConsent, type Consent } from "@/api/oauth";
import { useSpaces } from "@/api/spaces";
import { Page, PageHeader, Button, Card, OptionCard, ErrorBanner, Skeleton } from "@/components/ui";
import { OAUTH_SCOPE_READ, OAUTH_SCOPE_WRITE } from "@/config";
import { SpacePicker } from "@/features/spaces/SpacePicker";
import { t } from "@/i18n";
import { leave } from "@/lib/leave";

// Adapted from Armature's web/src/routes/oauth-authorize.tsx and Exciter's
// ConsentPage: the API decides what may be allowed, the page asks and goes back.

/** Where a person lets an app connect to the MCP endpoint as them (#449), for the authorize query as the app sent it. */
export function ConsentPage({ query }: { query: string }) {
  const { data, error, isLoading } = useConsent(query);
  const to = data?.redirectTo;
  // An answer for the app at once, such as a request it got wrong, goes straight back.
  useEffect(() => {
    if (to) leave(to);
  }, [to]);

  if (error) {
    return (
      <Page width="narrow" data-consent-error>
        <PageHeader title={t.oauth.titleRefused} />
        <ErrorBanner>{error.message}</ErrorBanner>
      </Page>
    );
  }
  if (isLoading || to || !data?.consent) {
    return (
      <Page width="narrow">
        <PageHeader title={t.oauth.titleWaiting} />
        <p role="status" className="text-sm text-ink-muted">
          {to ? t.oauth.leaving : t.oauth.loading}
        </p>
        {!to && <Skeleton />}
      </Page>
    );
  }
  return <ConsentCard consent={data.consent} query={query} />;
}

function ConsentCard({ consent, query }: { consent: Consent; query: string }) {
  const decide = useDecideConsent();
  const asksWrite = consent.scopes.includes(OAUTH_SCOPE_WRITE);
  const [write, setWrite] = useState(asksWrite);
  const [limited, setLimited] = useState(false);
  const [spaces, setSpaces] = useState<string[]>([]);
  const [missing, setMissing] = useState(false);
  const [leaving, setLeaving] = useState(false);
  const fields = decide.error instanceof ApiError ? decide.error.fields : {};
  const formError = decide.error && Object.keys(fields).length === 0 ? decide.error.message : null;
  const { client } = consent;

  function answer(allow: boolean) {
    if (allow && limited && spaces.length === 0) {
      setMissing(true);
      return;
    }
    const body = allow
      ? { query, decision: "allow", ...(asksWrite && !write ? { scope: OAUTH_SCOPE_READ } : {}), ...(limited ? { spaces } : {}) }
      : { query, decision: "deny" };
    decide.mutate(body, {
      onSuccess: (redirectTo) => {
        setLeaving(true);
        leave(redirectTo);
      },
    });
  }

  return (
    <Page width="narrow">
      <PageHeader title={t.oauth.title(client.name)} meta={client.clientUri} />
      <Card className="space-y-4 p-4" data-consent={client.id} data-consent-pending={consent.pending ? "true" : "false"}>
        <p className="text-base font-medium text-ink">{t.oauth.asks(client.name, consent.person)}</p>
        <div className="space-y-1 text-sm">
          {client.clientUri && (
            <p>
              <span className="font-medium text-ink">{t.oauth.appAddress}: </span>
              <span className="break-all font-mono text-ink" data-consent-address>
                {client.clientUri}
              </span>
            </p>
          )}
          <p className="text-ink-muted" data-consent-host>
            {t.oauth.goesBack(consent.redirectHost)}
          </p>
        </div>
        {consent.allowsApp && (
          <p className="rounded-control border border-border bg-surface-raised px-3 py-2 text-sm text-ink" data-consent-allows-app>
            {t.oauth.allowsApp(client.name)}
          </p>
        )}
        {consent.refusal && (
          <p className="rounded-control border border-border bg-surface-raised px-3 py-2 text-sm text-ink" data-consent-refusal>
            {consent.refusal}
          </p>
        )}
        {consent.mayAllow && (
          <>
            {asksWrite ? (
              <fieldset data-consent-access>
                <legend className="mb-2 text-sm font-medium text-ink">{t.oauth.access}</legend>
                <div role="radiogroup" aria-label={t.oauth.access} className="grid gap-2 sm:grid-cols-2">
                  <OptionCard
                    checked={write}
                    onSelect={() => setWrite(true)}
                    title={t.oauth.accessWrite}
                    description={t.oauth.accessWriteHint}
                    data-scope={OAUTH_SCOPE_WRITE}
                  />
                  <OptionCard
                    checked={!write}
                    onSelect={() => setWrite(false)}
                    title={t.oauth.accessRead}
                    description={t.oauth.accessReadHint}
                    data-scope={OAUTH_SCOPE_READ}
                  />
                </div>
              </fieldset>
            ) : (
              <p className="text-sm text-ink-muted" data-consent-read-only>
                {t.oauth.askedRead}
              </p>
            )}
            {fields.scope && <ErrorBanner>{fields.scope}</ErrorBanner>}
            <fieldset data-consent-reach>
              <legend className="mb-2 text-sm font-medium text-ink">{t.oauth.reach}</legend>
              <div role="radiogroup" aria-label={t.oauth.reach} className="grid gap-2 sm:grid-cols-2">
                <OptionCard
                  checked={!limited}
                  onSelect={() => setLimited(false)}
                  title={t.oauth.reachAll}
                  description={t.oauth.reachAllHint}
                  data-reach="all"
                />
                <OptionCard
                  checked={limited}
                  onSelect={() => setLimited(true)}
                  title={t.oauth.reachSome}
                  description={t.oauth.reachSomeHint}
                  data-reach="some"
                />
              </div>
            </fieldset>
            {limited && (
              <ConsentSpaces
                picked={spaces}
                onChange={(keys) => {
                  setSpaces(keys);
                  setMissing(false);
                }}
              />
            )}
            {missing && <ErrorBanner>{t.oauth.spacesMissing}</ErrorBanner>}
            {fields.spaces && <ErrorBanner>{fields.spaces}</ErrorBanner>}
          </>
        )}
        {formError && <ErrorBanner>{formError}</ErrorBanner>}
        <div className="flex flex-wrap items-center gap-3">
          {consent.mayAllow && (
            <Button
              loading={decide.isPending && decide.variables?.decision === "allow"}
              disabled={leaving}
              onClick={() => answer(true)}
              data-action="consent-allow"
            >
              {t.oauth.allow}
            </Button>
          )}
          {consent.mayCancel && (
            <Button
              variant="secondary"
              loading={decide.isPending && decide.variables?.decision === "deny"}
              disabled={leaving}
              onClick={() => answer(false)}
              data-action="consent-cancel"
            >
              {t.oauth.cancel}
            </Button>
          )}
          <p role="status" className="text-sm text-ink-muted empty:hidden">
            {leaving ? t.oauth.leaving : ""}
          </p>
        </div>
        {consent.mayAllow && <p className="text-xs text-ink-muted">{t.oauth.disconnect}</p>}
      </Card>
    </Page>
  );
}

// The spaces are asked for only once the person limits the app to some.
function ConsentSpaces({ picked, onChange }: { picked: string[]; onChange: (keys: string[]) => void }) {
  const { data: choosable = [] } = useSpaces();
  return (
    <SpacePicker
      spaces={choosable}
      picked={picked}
      onChange={onChange}
      legend={t.oauth.spaces}
      hint={t.oauth.spacesHint}
      hintId="consent-spaces-hint"
      data-consent-spaces
    />
  );
}
