import { localDateFormat } from "@/lib/format";
import { useEffect, useRef, useState, type FormEvent } from "react";
import { ApiError } from "@/api/client";
import { useArmatureAccount, useCheckArmature, useConnectArmature, useDisconnectArmature, type ArmatureAccount } from "@/api/armature";
import { Button, ButtonLink, Card, ErrorBanner, Field, SectionTitle, Skeleton } from "@/components/ui";
import { Icon } from "@/components/icons";
import { ARMATURE_SECTION_ID, ARMATURE_TOKEN_PREFIX, type ArmatureOutcome } from "@/config";
import { t } from "@/i18n";
import { appConnectHref, tokensUrl } from "./connectLink";

const when = localDateFormat({ dateStyle: "medium", timeStyle: "short" });

/**
 * The caller's own Armature account: connect it with one click where Armature offers connected apps, or paste a token;
 * see whom it acts as, check it, disconnect. Reached from a connect hint, it focuses the token field and, once connected,
 * hands over to onReturn; returnTo is where a one-click connect comes back to, and outcome how the last one ended.
 */
export function AccountSection({
  focusToken = false,
  onReturn,
  returnTo,
  outcome,
}: {
  focusToken?: boolean;
  onReturn?: () => void;
  returnTo?: string;
  outcome?: ArmatureOutcome;
}) {
  const { data: account, isLoading, error } = useArmatureAccount();
  const check = useCheckArmature();
  const disconnect = useDisconnectArmature();
  const [notice, setNotice] = useState(outcome === "connected" ? t.armature.account.outcomes.connected : "");
  const [shownOutcome, setShownOutcome] = useState(outcome && outcome !== "connected" ? outcome : undefined);

  function forget() {
    const viaApp = account?.via === "app";
    if (!window.confirm(viaApp ? t.armature.account.confirmDisconnectApp : t.armature.account.confirmDisconnect)) return;
    setNotice("");
    setShownOutcome(undefined);
    disconnect.mutate(undefined, { onSuccess: () => setNotice(t.armature.account.disconnected) });
  }

  function checkNow() {
    setNotice("");
    check.mutate(undefined, { onSuccess: (checked) => checked.status === "ok" && setNotice(t.armature.account.checked) });
  }

  const failure = error ?? check.error ?? disconnect.error;
  return (
    <section id={ARMATURE_SECTION_ID} aria-labelledby="armature-account" className="scroll-mt-16 space-y-3" data-armature-account>
      <SectionTitle id="armature-account">{t.armature.title}</SectionTitle>
      <p className="text-sm text-ink-muted">{t.armature.account.intro}</p>
      <p role="status" className="text-sm text-ink-muted empty:hidden" data-armature-notice>
        {notice}
      </p>
      {failure && <ErrorBanner>{failure.message}</ErrorBanner>}
      {shownOutcome && (
        <div data-armature-outcome={shownOutcome}>
          <ErrorBanner>{outcomeSentence(shownOutcome, account)}</ErrorBanner>
        </div>
      )}
      {isLoading || !account ? (
        isLoading && <Skeleton />
      ) : !account.configured ? (
        <Card className="p-4 text-sm text-ink" data-armature-status="not_configured">
          {t.armature.account.notConfigured}
        </Card>
      ) : (
        <Card className="space-y-4 p-4" data-armature-status={account.status} data-armature-via={account.via ?? undefined}>
          {account.connected && <Connected account={account} />}
          {account.connected && (
            <div className="flex flex-wrap gap-2">
              <Button variant="secondary" size="sm" onClick={checkNow} loading={check.isPending} data-action="check-armature">
                {t.armature.account.check}
              </Button>
              <Button variant="ghost" size="sm" onClick={forget} loading={disconnect.isPending} data-action="disconnect-armature-account">
                {t.armature.account.disconnect}
              </Button>
            </div>
          )}
          {account.status !== "ok" &&
            (account.app.offered ? (
              <OneClick account={account} focusToken={focusToken} returnTo={returnTo} returns={Boolean(onReturn)} onConnected={() => onReturn?.()} />
            ) : (
              <>
                {account.app.refusal && (
                  <p className="text-sm text-ink-muted" data-armature-refusal>
                    {account.app.refusal}
                  </p>
                )}
                <TokenForm
                  account={account}
                  focus={focusToken}
                  returns={Boolean(onReturn)}
                  onConnected={() => {
                    setNotice("");
                    onReturn?.();
                  }}
                />
              </>
            ))}
        </Card>
      )}
    </section>
  );
}

// A refusal of the registration is said in Armature's own words where the
// account carries them.
function outcomeSentence(outcome: ArmatureOutcome, account: ArmatureAccount | undefined): string {
  if (outcome === "refused" && account?.app.refusal) return account.app.refusal;
  return t.armature.account.outcomes[outcome];
}

// The token form stays behind a disclosure: the button is what most people
// want, and a token is for an Armature that has not allowed Stator yet.
function OneClick({
  account,
  focusToken,
  returnTo,
  returns,
  onConnected,
}: {
  account: ArmatureAccount;
  focusToken: boolean;
  returnTo?: string;
  returns: boolean;
  onConnected: () => void;
}) {
  const [withToken, setWithToken] = useState(false);
  return (
    <div className="space-y-3" data-armature-one-click>
      <p className="text-sm text-ink-muted">{t.armature.account.appIntro}</p>
      {account.app.pending && (
        <p className="text-sm text-ink" data-armature-app-pending>
          {t.armature.account.appPending}
        </p>
      )}
      {returnTo && <p className="text-sm text-ink-muted">{t.armature.account.appReturns}</p>}
      <ButtonLink variant="primary" href={appConnectHref(returnTo)} data-action="connect-armature-app">
        {t.armature.account.connectApp}
      </ButtonLink>
      <div>
        <Button variant="link" aria-expanded={withToken} onClick={() => setWithToken((open) => !open)} data-action="use-armature-token">
          {t.armature.account.useToken}
        </Button>
      </div>
      {withToken && <TokenForm account={account} focus={focusToken || withToken} returns={returns} onConnected={onConnected} />}
    </div>
  );
}

function Connected({ account }: { account: ArmatureAccount }) {
  const viaApp = account.via === "app";
  return (
    <div className="space-y-1 text-sm">
      {account.user && (
        <p className="text-ink" data-armature-user>
          {t.armature.account.connectedAs(account.user.name, account.user.email)}
        </p>
      )}
      {viaApp && <p className="text-ink-muted">{t.armature.account.viaApp}</p>}
      {account.status === "rejected" && <ErrorBanner>{viaApp ? t.armature.account.rejectedApp : t.armature.account.rejected}</ErrorBanner>}
      {account.status === "unreachable" && <p className="text-ink-muted">{t.armature.account.unreachable}</p>}
      {account.checkedAt && <p className="text-ink-muted">{t.armature.account.checkedAt(when.format(new Date(account.checkedAt)))}</p>}
    </div>
  );
}

// The token field is only offered while there is no working token: a stored
// one is never shown, and a rejected one is replaced by pasting a new one.
function TokenForm({ account, focus, returns, onConnected }: { account: ArmatureAccount; focus: boolean; returns: boolean; onConnected: () => void }) {
  const connect = useConnectArmature();
  const [token, setToken] = useState("");
  const field = useRef<HTMLInputElement>(null);
  useEffect(() => {
    if (focus) field.current?.focus();
  }, [focus]);
  const fields = connect.error instanceof ApiError ? connect.error.fields : {};
  const formError = connect.error && Object.keys(fields).length === 0 ? connect.error.message : null;

  function submit(event: FormEvent) {
    event.preventDefault();
    connect.mutate(token.trim(), {
      onSuccess: () => {
        setToken("");
        onConnected();
      },
    });
  }

  return (
    <form onSubmit={submit} noValidate className="space-y-3" data-armature-token-form>
      <p className="text-sm text-ink-muted">{t.armature.account.steps}</p>
      {account.baseUrl && (
        <a
          href={tokensUrl(account.baseUrl)}
          target="_blank"
          rel="noreferrer"
          className="inline-flex items-center gap-1 text-sm text-accent hover:underline"
          data-action="open-armature-tokens"
        >
          {t.armature.account.openTokens}
          <Icon.External aria-hidden="true" />
        </a>
      )}
      <Field
        ref={field}
        label={account.connected ? t.armature.account.replace : t.armature.account.token}
        type="password"
        autoComplete="off"
        placeholder={`${ARMATURE_TOKEN_PREFIX}...`}
        value={token}
        error={fields.token}
        onChange={(event) => setToken(event.target.value)}
      />
      {formError && <ErrorBanner>{formError}</ErrorBanner>}
      {returns && <p className="text-sm text-ink-muted">{t.armature.account.returns}</p>}
      <Button type="submit" loading={connect.isPending} disabled={!token.trim()} data-action="connect-armature">
        {t.armature.account.connect}
      </Button>
    </form>
  );
}
