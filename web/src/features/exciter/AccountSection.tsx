import { useEffect, useRef, useState, type FormEvent } from "react";
import { ApiError } from "@/api/client";
import { useCheckExciter, useConnectExciter, useDisconnectExciter, useExciterAccount, type ExciterAccount } from "@/api/exciter";
import { Button, Card, ErrorBanner, Field, SectionTitle, Skeleton } from "@/components/ui";
import { Icon } from "@/components/icons";
import { EXCITER_SECTION_ID, EXCITER_TOKEN_PREFIX, EXCITER_TOKENS_PATH } from "@/config";
import { t } from "@/i18n";
import { localDateFormat } from "@/lib/format";

const when = localDateFormat({ dateStyle: "medium", timeStyle: "short" });

/** Exciter's page for making tokens, under the address Stator is connected to. */
export function exciterTokensUrl(baseUrl: string): string {
  return `${baseUrl.replace(/\/+$/, "")}${EXCITER_TOKENS_PATH}`;
}

/**
 * The caller's own Exciter account, as the Armature section beside it: paste a token, see whom it acts as,
 * check it, disconnect. Reached from a connect hint, it focuses the token field and then hands over to onReturn.
 */
export function ExciterAccountSection({ focusToken = false, onReturn }: { focusToken?: boolean; onReturn?: () => void }) {
  const { data: account, isLoading, error } = useExciterAccount();
  const check = useCheckExciter();
  const disconnect = useDisconnectExciter();
  const [notice, setNotice] = useState("");
  const a = t.exciter.account;

  function forget() {
    if (!window.confirm(a.confirmDisconnect)) return;
    setNotice("");
    disconnect.mutate(undefined, { onSuccess: () => setNotice(a.disconnected) });
  }

  function checkNow() {
    setNotice("");
    check.mutate(undefined, { onSuccess: (checked) => checked.status === "ok" && setNotice(a.checked) });
  }

  const failure = error ?? check.error ?? disconnect.error;
  return (
    <section id={EXCITER_SECTION_ID} aria-labelledby="exciter-account" className="scroll-mt-16 space-y-3" data-exciter-account>
      <SectionTitle id="exciter-account">{t.exciter.title}</SectionTitle>
      <p className="text-sm text-ink-muted">{a.intro}</p>
      <p role="status" className="text-sm text-ink-muted empty:hidden" data-exciter-notice>
        {notice}
      </p>
      {failure && <ErrorBanner>{failure.message}</ErrorBanner>}
      {isLoading || !account ? (
        isLoading && <Skeleton />
      ) : !account.configured ? (
        <Card className="p-4 text-sm text-ink" data-exciter-status="not_configured">
          {a.notConfigured}
        </Card>
      ) : (
        <Card className="space-y-4 p-4" data-exciter-status={account.status}>
          {account.connected && <Connected account={account} />}
          {account.connected && (
            <div className="flex flex-wrap gap-2">
              <Button variant="secondary" size="sm" onClick={checkNow} loading={check.isPending} data-action="check-exciter">
                {a.check}
              </Button>
              <Button variant="ghost" size="sm" onClick={forget} loading={disconnect.isPending} data-action="disconnect-exciter-account">
                {a.disconnect}
              </Button>
            </div>
          )}
          {account.status !== "ok" && (
            <TokenForm
              account={account}
              focus={focusToken}
              returns={Boolean(onReturn)}
              onConnected={() => {
                setNotice("");
                onReturn?.();
              }}
            />
          )}
        </Card>
      )}
    </section>
  );
}

function Connected({ account }: { account: ExciterAccount }) {
  const a = t.exciter.account;
  return (
    <div className="space-y-1 text-sm">
      {account.user && (
        <p className="text-ink" data-exciter-user>
          {a.connectedAs(account.user.name, account.user.email)}
        </p>
      )}
      {account.status === "rejected" && <ErrorBanner>{a.rejected}</ErrorBanner>}
      {account.status === "unreachable" && <p className="text-ink-muted">{a.unreachable}</p>}
      {account.checkedAt && <p className="text-ink-muted">{a.checkedAt(when.format(new Date(account.checkedAt)))}</p>}
    </div>
  );
}

// The token field is only offered while there is no working token: a stored
// one is never shown, and a rejected one is replaced by pasting a new one.
function TokenForm({ account, focus, returns, onConnected }: { account: ExciterAccount; focus: boolean; returns: boolean; onConnected: () => void }) {
  const connect = useConnectExciter();
  const [token, setToken] = useState("");
  const field = useRef<HTMLInputElement>(null);
  const a = t.exciter.account;
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
    <form onSubmit={submit} noValidate className="space-y-3" data-exciter-token-form>
      <p className="text-sm text-ink-muted">{a.steps}</p>
      {account.baseUrl && (
        <a
          href={exciterTokensUrl(account.baseUrl)}
          target="_blank"
          rel="noreferrer"
          className="inline-flex items-center gap-1 text-sm text-accent hover:underline"
          data-action="open-exciter-tokens"
        >
          {a.openTokens}
          <Icon.External aria-hidden="true" />
        </a>
      )}
      <Field
        ref={field}
        label={account.connected ? a.replace : a.token}
        type="password"
        autoComplete="off"
        placeholder={`${EXCITER_TOKEN_PREFIX}...`}
        value={token}
        error={fields.token}
        onChange={(event) => setToken(event.target.value)}
      />
      {formError && <ErrorBanner>{formError}</ErrorBanner>}
      {returns && <p className="text-sm text-ink-muted">{a.returns}</p>}
      <Button type="submit" loading={connect.isPending} disabled={!token.trim()} data-action="connect-exciter">
        {a.connect}
      </Button>
    </form>
  );
}
