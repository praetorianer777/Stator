import { useEffect, useRef, useState, type FormEvent } from "react";
import { ApiError } from "@/api/client";
import {
  useCheckExciterAddress,
  useExciterAccount,
  useExciterConnection,
  useRemoveExciterApp,
  useRemoveExciterConnection,
  useSaveExciterApp,
  useSaveExciterConnection,
  type ExciterConnection,
  type ExciterConnectionCheck,
  type ExciterConnectionInput,
  type ExciterPartner,
} from "@/api/exciter";
import { Button, Card, Checkbox, ErrorBanner, Field, SectionTitle, Skeleton, useToast } from "@/components/ui";
import { Icon } from "@/components/icons";
import { COPY_FEEDBACK_MS, EXCITER_CLIENT_SECRET_PREFIX, EXCITER_WEBHOOK_SECRET_PREFIX } from "@/config";
import { ConnectExciterLink } from "./connectLink";
import { t } from "@/i18n";
import { localDateFormat } from "@/lib/format";
import { useConfirm } from "@/features/shell/ConfirmProvider";

/** The Exciter installation Stator is connected to and its webhook, for administrators, as Armature's settings are. */
export function ExciterConnectionSettings() {
  const { data, isLoading, error } = useExciterConnection();
  // Held here rather than in the form: a save remounts the form, and what it
  // says about that save has to outlive the remount.
  const save = useSaveExciterConnection();
  const remove = useRemoveExciterConnection();
  const toast = useToast();
  const confirm = useConfirm();
  const x = t.exciter;
  if (isLoading) return <Skeleton />;
  if (error) return <ErrorBanner>{error instanceof ApiError && error.status === 403 ? x.notAdmin : error.message}</ErrorBanner>;
  const connection = data ?? null;

  async function disconnect() {
    if (!(await confirm({ noun: "Exciter", verb: t.kit.confirm.verbs.disconnect, body: x.confirmDisconnect }))) return;
    remove.mutate(undefined, { onSuccess: () => toast.success(x.disconnected) });
  }

  return (
    <div className="space-y-6">
      <ConnectionForm key={connection?.updatedAt ?? "new"} connection={connection} save={save} onSaved={toast.success} />
      {connection && <MembersNext />}
      {connection && <Capabilities partner={connection.partner} />}
      {connection && <PartnerApp connection={connection} />}
      {connection && <Webhook connection={connection} />}
      {connection && (
        <section aria-labelledby="exciter-disconnect" className="space-y-2">
          <SectionTitle id="exciter-disconnect">{x.disconnectTitle}</SectionTitle>
          <p className="text-sm text-ink-muted">{x.disconnectIntro}</p>
          {remove.error && <ErrorBanner>{remove.error.message}</ErrorBanner>}
          <Button variant="danger" onClick={() => void disconnect()} loading={remove.isPending} data-action="disconnect-exciter">
            {x.disconnect}
          </Button>
        </section>
      )}
    </div>
  );
}

/** What the check found at the address, in the words the capabilities section uses once saved. */
function foundSentence(found: ExciterConnectionCheck): string {
  const p = found.partner;
  const answers = p.version ? t.exciter.checkFound(p.version) : t.exciter.checkFoundUnversioned;
  return p.missing.length > 0 ? `${answers} ${t.exciter.checkMissing(p.missing.length)}` : answers;
}

// The webhook secret is write-only: it never comes back, and a blank field
// keeps the stored one, so saving the address cannot erase it.
function ConnectionForm({
  connection,
  save,
  onSaved,
}: {
  connection: ExciterConnection | null;
  save: ReturnType<typeof useSaveExciterConnection>;
  onSaved: (notice: string) => void;
}) {
  const x = t.exciter;
  const [baseUrl, setBaseUrl] = useState(connection?.baseUrl ?? "");
  const [secret, setSecret] = useState("");
  const [forget, setForget] = useState(false);
  const check = useCheckExciterAddress();
  const fields = save.error instanceof ApiError ? save.error.fields : {};
  const formError = save.error && Object.keys(fields).length === 0 ? save.error.message : null;
  const checkError = check.error instanceof ApiError ? (check.error.fields.baseUrl ?? check.error.message) : check.error?.message;

  function submit(event: FormEvent) {
    event.preventDefault();
    onSaved("");
    const body: ExciterConnectionInput = { baseUrl: baseUrl.trim() };
    if (secret.trim()) body.webhookSecret = secret.trim();
    else if (forget) body.webhookSecret = "";
    save.mutate(body, { onSuccess: () => onSaved(x.saved) });
  }

  return (
    <form onSubmit={submit} noValidate aria-label={x.title} data-exciter-connection>
      <Card className="space-y-4 p-4">
        <p className="text-sm text-ink-muted">{x.intro}</p>
        <Field
          label={x.baseUrl}
          value={baseUrl}
          type="url"
          placeholder="https://exciter.example.com"
          hint={x.baseUrlHint}
          error={fields.baseUrl ?? checkError}
          onChange={(event) => {
            setBaseUrl(event.target.value);
            check.reset();
          }}
        />
        <div className="flex flex-wrap items-center gap-2">
          <Button
            variant="secondary"
            size="sm"
            onClick={() => check.mutate(baseUrl.trim())}
            disabled={baseUrl.trim() === ""}
            loading={check.isPending}
            data-action="check-exciter-address"
          >
            {x.check}
          </Button>
          <p role="status" className="text-sm text-ink-muted empty:hidden" data-exciter-check={check.data ? "found" : undefined}>
            {check.data ? foundSentence(check.data) : ""}
          </p>
        </div>
        <Field
          label={x.webhookSecret}
          type="password"
          autoComplete="off"
          value={secret}
          placeholder={connection?.webhookSecretSet ? x.secretStored : `${EXCITER_WEBHOOK_SECRET_PREFIX}...`}
          error={fields.webhookSecret}
          onChange={(event) => setSecret(event.target.value)}
        />
        {connection?.webhookSecretSet && <Checkbox label={x.forgetSecret} checked={forget} onChange={(event) => setForget(event.target.checked)} />}
        {connection && (
          <p className="text-sm text-ink-muted" data-exciter-connected>
            {x.connectedCount(connection.connected)}
          </p>
        )}
        {connection && <p className="text-xs text-ink-muted">{x.movesForget}</p>}
        {formError && <ErrorBanner>{formError}</ErrorBanner>}
        <Button type="submit" loading={save.isPending} data-action="save-exciter">
          {connection ? x.save : x.connect}
        </Button>
      </Card>
    </form>
  );
}

/** Once Exciter is connected, what each member does to see its requirements, and the way for the administrator to do it too. */
function MembersNext() {
  const { data: own } = useExciterAccount();
  const works = own?.connected && own.status !== "rejected";
  return (
    <section aria-labelledby="exciter-members" className="space-y-2" data-exciter-members="">
      <SectionTitle id="exciter-members">{t.exciter.members.title}</SectionTitle>
      <p className="text-sm text-ink-muted">{t.exciter.members.intro}</p>
      {own && !works && <ConnectExciterLink className="inline-flex text-sm text-accent hover:underline">{t.exciter.members.own}</ConnectExciterLink>}
    </section>
  );
}

/** What the connected Exciter offers, by exact capability name, and what Stator turns off for what it lacks. */
function Capabilities({ partner }: { partner: ExciterPartner }) {
  const c = t.exciter.capabilities;
  const known = partner.status === "ok";
  const summary = !known ? c.unreachable : partner.missing.length === 0 ? c.all : null;
  return (
    <section
      aria-labelledby="exciter-capabilities"
      className="space-y-2"
      data-exciter-capabilities={!known ? "unknown" : partner.missing.length > 0 ? "missing" : "all"}
    >
      <SectionTitle id="exciter-capabilities">{c.title}</SectionTitle>
      <p className="text-sm text-ink-muted">{c.intro}</p>
      {partner.version && <p className="text-sm text-ink">{c.version(partner.version)}</p>}
      {summary && <p className="text-sm text-ink">{summary}</p>}
      {partner.missing.length > 0 && (
        <ul className="list-disc space-y-1 pl-5 text-sm text-ink">
          {partner.missing.map((m) => (
            <li key={m.feature} data-missing={m.feature}>
              {c.missing(c.features[m.feature], m.capability)}
            </li>
          ))}
        </ul>
      )}
      {partner.capabilities.length > 0 && (
        <ul className="flex flex-wrap gap-1.5" aria-label={c.title}>
          {partner.capabilities.map((name) => (
            <li key={name}>
              <code className="rounded bg-surface-raised px-1 font-mono text-xs text-ink">{name}</code>
            </li>
          ))}
        </ul>
      )}
    </section>
  );
}

/**
 * Stator as a partner app of Exciter, which connects each person at sign-in by exchanging their access token:
 * where it stands, and the client id and secret Exciter's administrator registered it with.
 */
function PartnerApp({ connection }: { connection: ExciterConnection }) {
  const p = t.exciter.app;
  const app = connection.app;
  const save = useSaveExciterApp();
  const remove = useRemoveExciterApp();
  const [clientId, setClientId] = useState("");
  const [clientSecret, setClientSecret] = useState("");
  const toast = useToast();
  const confirm = useConfirm();
  const fields = save.error instanceof ApiError ? save.error.fields : {};
  const formError = save.error && Object.keys(fields).length === 0 ? save.error.message : null;
  const state = app.exchanging ? "exchanging" : !app.offered ? "not_offered" : app.clientId ? "off" : "missing";

  function submit(event: FormEvent) {
    event.preventDefault();
    save.mutate(
      { clientId: clientId.trim(), clientSecret: clientSecret.trim() },
      {
        onSuccess: () => {
          setClientId("");
          setClientSecret("");
          toast.success(p.saved);
        },
      },
    );
  }

  async function forget() {
    if (!(await confirm({ noun: t.kit.confirm.things.app, verb: t.kit.confirm.verbs.remove, body: p.confirmRemove }))) return;
    remove.mutate(undefined, { onSuccess: () => toast.success(p.removed) });
  }

  return (
    <section aria-labelledby="exciter-app" className="space-y-3" data-exciter-app={state}>
      <SectionTitle id="exciter-app">{p.title}</SectionTitle>
      <p className="text-sm text-ink-muted">{p.intro}</p>
      <p className="text-sm text-ink" data-exciter-app-state>
        {p.states[state]}
      </p>
      {app.clientId && <p className="text-sm text-ink-muted">{p.clientIdShown(app.clientId)}</p>}
      {remove.error && <ErrorBanner>{remove.error.message}</ErrorBanner>}
      <form onSubmit={submit} noValidate className="space-y-3 rounded-control border border-border p-4" aria-label={p.formTitle} data-exciter-app-form>
        <Field label={p.clientId} autoComplete="off" value={clientId} error={fields.clientId} onChange={(event) => setClientId(event.target.value)} />
        <Field
          label={p.clientSecret}
          type="password"
          autoComplete="off"
          placeholder={`${EXCITER_CLIENT_SECRET_PREFIX}...`}
          value={clientSecret}
          error={fields.clientSecret}
          onChange={(event) => setClientSecret(event.target.value)}
        />
        {formError && <ErrorBanner>{formError}</ErrorBanner>}
        <div className="flex flex-wrap gap-2">
          <Button type="submit" loading={save.isPending} disabled={!clientId.trim() || !clientSecret.trim()} data-action="save-exciter-app">
            {p.save}
          </Button>
          {app.clientId && (
            <Button variant="ghost" onClick={() => void forget()} loading={remove.isPending} data-action="forget-exciter-app">
              {p.remove}
            </Button>
          )}
        </div>
      </form>
    </section>
  );
}

const deliveryTime = localDateFormat({ dateStyle: "medium", timeStyle: "short" });

function Webhook({ connection }: { connection: ExciterConnection }) {
  const x = t.exciter;
  const [said, setSaid] = useState("");
  const timer = useRef<ReturnType<typeof setTimeout> | undefined>(undefined);
  useEffect(() => () => clearTimeout(timer.current), []);
  const last = connection.lastDelivery;

  async function copy() {
    let message = x.copied;
    try {
      await navigator.clipboard.writeText(connection.webhookUrl);
    } catch {
      message = x.copyFailed;
    }
    setSaid(message);
    clearTimeout(timer.current);
    timer.current = setTimeout(() => setSaid(""), COPY_FEEDBACK_MS);
  }

  return (
    <section aria-labelledby="exciter-webhook" className="space-y-3">
      <SectionTitle id="exciter-webhook">{x.webhookTitle}</SectionTitle>
      <p className="text-sm text-ink-muted">{x.webhookIntro}</p>
      <ol className="list-decimal space-y-1 pl-5 text-sm text-ink">
        {x.webhookSteps.map((step) => (
          <li key={step}>{step}</li>
        ))}
      </ol>
      <Field label={x.webhookUrl} value={connection.webhookUrl} readOnly className="font-mono text-xs" onFocus={(event) => event.target.select()} />
      <div className="flex flex-wrap items-center gap-2">
        <Button variant="secondary" size="sm" icon={<Icon.Copy />} onClick={copy} data-action="copy-exciter-webhook-url">
          {x.copy}
        </Button>
        <p role="status" className="text-xs text-ink-muted" data-copy-status>
          {said}
        </p>
      </div>
      <p className="text-sm text-ink-muted" data-last-delivery>
        {last ? x.lastDelivery.taken(last.topic, deliveryTime.format(new Date(last.receivedAt))) : x.lastDelivery.none}
      </p>
      <div>
        <h3 className="text-xs font-medium text-ink-muted">{x.topics}</h3>
        <ul className="mt-1 flex flex-wrap gap-1.5" aria-label={x.topics}>
          {connection.webhookTopics.map((topic) => (
            <li key={topic}>
              <code className="rounded bg-surface-raised px-1 font-mono text-xs text-ink">{topic}</code>
            </li>
          ))}
        </ul>
      </div>
    </section>
  );
}
