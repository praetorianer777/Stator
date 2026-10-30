import { useState, type FormEvent } from "react";
import { ApiError } from "@/api/client";
import { useRemoveArmatureApp, useSaveArmatureApp, type ArmatureConnection } from "@/api/armature";
import { Button, ErrorBanner, Field, SectionTitle, useToast } from "@/components/ui";
import { ARMATURE_CLIENT_SECRET_PREFIX } from "@/config";
import { t } from "@/i18n";
import { useConfirm } from "@/features/shell/ConfirmProvider";

/**
 * Stator as a connected app of the organization's Armature, for administrators: where it stands, Armature's
 * sentence when it would not register Stator, and the form to paste an app registered by hand.
 */
export function ConnectedApp({ connection }: { connection: ArmatureConnection }) {
  const app = connection.app;
  const remove = useRemoveArmatureApp();
  const toast = useToast();
  const confirm = useConfirm();
  // Pasting is what a refusal asks for; otherwise it is there for an
  // administrator who registered Stator in Armature already.
  const [pasting, setPasting] = useState(app.status === "refused");

  async function forget() {
    if (!(await confirm({ noun: t.kit.confirm.things.app, verb: t.kit.confirm.verbs.remove, body: t.armature.app.confirmRemove }))) return;
    remove.mutate(undefined, { onSuccess: () => toast.success(t.armature.app.removed) });
  }

  return (
    <section aria-labelledby="armature-app" className="space-y-3" data-armature-app={app.status}>
      <SectionTitle id="armature-app">{t.armature.app.title}</SectionTitle>
      <p className="text-sm text-ink-muted">{t.armature.app.intro}</p>
      {!app.offered ? (
        <p className="text-sm text-ink" data-armature-app-offered="no">
          {t.armature.app.notOffered}
        </p>
      ) : (
        <div className="space-y-1 text-sm text-ink">
          <p data-armature-app-status>{t.armature.app.status[app.status]}</p>
          {app.refusal && (
            <blockquote className="border-l-2 border-border pl-3 text-ink-muted" data-armature-app-refusal>
              {app.refusal}
            </blockquote>
          )}
          {app.clientId && (
            <p className="text-ink-muted">
              {t.armature.app.clientIdShown(app.clientId)} {app.registeredBy && t.armature.app.registeredBy[app.registeredBy]}
            </p>
          )}
        </div>
      )}
      {remove.error && <ErrorBanner>{remove.error.message}</ErrorBanner>}
      <div className="flex flex-wrap gap-2">
        {!pasting && (
          <Button variant="secondary" size="sm" onClick={() => setPasting(true)} data-action="paste-armature-app">
            {t.armature.app.pasteTitle}
          </Button>
        )}
        {app.status !== "none" && (
          <Button variant="ghost" size="sm" onClick={() => void forget()} loading={remove.isPending} data-action="forget-armature-app">
            {t.armature.app.remove}
          </Button>
        )}
      </div>
      {pasting && (
        <PasteForm
          redirectUri={app.redirectUri}
          onSaved={() => {
            setPasting(false);
            toast.success(t.armature.app.saved);
          }}
        />
      )}
    </section>
  );
}

function PasteForm({ redirectUri, onSaved }: { redirectUri: string; onSaved: () => void }) {
  const save = useSaveArmatureApp();
  const [clientId, setClientId] = useState("");
  const [clientSecret, setClientSecret] = useState("");
  const fields = save.error instanceof ApiError ? save.error.fields : {};
  const formError = save.error && Object.keys(fields).length === 0 ? save.error.message : null;

  function submit(event: FormEvent) {
    event.preventDefault();
    save.mutate({ clientId: clientId.trim(), clientSecret: clientSecret.trim() }, { onSuccess: onSaved });
  }

  return (
    <form
      onSubmit={submit}
      noValidate
      className="space-y-3 rounded-control border border-border p-4"
      aria-label={t.armature.app.pasteTitle}
      data-armature-app-form
    >
      <p className="text-sm text-ink-muted">{t.armature.app.pasteIntro}</p>
      <Field label={t.armature.app.redirectUri} value={redirectUri} readOnly onFocus={(event) => event.currentTarget.select()} />
      <Field
        label={t.armature.app.clientId}
        autoComplete="off"
        value={clientId}
        error={fields.clientId}
        onChange={(event) => setClientId(event.target.value)}
      />
      <Field
        label={t.armature.app.clientSecret}
        type="password"
        autoComplete="off"
        placeholder={`${ARMATURE_CLIENT_SECRET_PREFIX}...`}
        hint={t.armature.app.clientSecretHint}
        value={clientSecret}
        error={fields.clientSecret}
        onChange={(event) => setClientSecret(event.target.value)}
      />
      {formError && <ErrorBanner>{formError}</ErrorBanner>}
      <Button type="submit" loading={save.isPending} disabled={!clientId.trim()} data-action="save-armature-app">
        {t.armature.app.save}
      </Button>
    </form>
  );
}
