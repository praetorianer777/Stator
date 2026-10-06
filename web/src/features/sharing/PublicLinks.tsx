import { useEffect, useRef, useState, type FormEvent } from "react";
import { useMe } from "@/api/auth";
import { ApiError } from "@/api/client";
import { useCreatePageLink, usePageLinks, useRevokePageLink, type PublicLink, type PublicLinkRefusal } from "@/api/public";
import { Icon } from "@/components/icons";
import { Button, ErrorBanner, Field, Select, Skeleton } from "@/components/ui";
import { COPY_FEEDBACK_MS, DAY_MS, PUBLIC_LINK_DEFAULT_EXPIRY_DAYS, PUBLIC_LINK_EXPIRY_DAYS, PUBLIC_LINK_LABEL_MAX_LENGTH } from "@/config";
import { t } from "@/i18n";
import { localDateFormat } from "@/lib/format";

const day = localDateFormat({ dateStyle: "medium" });

/** When a link made now for that many days runs out; none for a link that works until it is revoked. */
export function linkExpiry(days: number, now: Date = new Date()): string | undefined {
  return days === 0 ? undefined : new Date(now.getTime() + days * DAY_MS).toISOString();
}

/** Why no link can be made, as the dialog says it; a page with as many links as it may says so beside them. */
export function refusalText(refusal: PublicLinkRefusal, max: number): string {
  return refusal === "full" ? t.publicLinks.full(max) : t.publicLinks.refusals[refusal];
}

/**
 * A page's public links in the share dialog: made and revoked by whoever may
 * edit the page, each opening this page alone to anybody who holds it.
 */
export function PublicLinks({ pageId }: { pageId: string }) {
  const { data, error, refetch } = usePageLinks(pageId);
  const [fresh, setFresh] = useState<string>();
  return (
    <section className="space-y-3 border-t border-border pt-4" aria-labelledby="public-links-title" data-public-links="">
      <h3 id="public-links-title" className="text-sm font-semibold text-ink">
        {t.publicLinks.title}
      </h3>
      <p className="text-sm text-ink-muted">{t.publicLinks.intro}</p>
      {error ? (
        <ErrorBanner onRetry={() => void refetch()}>{error.message}</ErrorBanner>
      ) : !data ? (
        <Skeleton />
      ) : data.refusal === "cannotManage" ? (
        <p className="text-sm text-ink-muted" data-public-links-refusal="cannotManage">
          {t.publicLinks.refusals.cannotManage}
        </p>
      ) : (
        <>
          {fresh && <FreshLink path={fresh} />}
          {data.links.length > 0 && <LinkList pageId={pageId} links={data.links} />}
          {data.refusal ? (
            <p className="text-sm text-ink-muted" data-public-links-refusal={data.refusal}>
              {refusalText(data.refusal, data.max)}
            </p>
          ) : (
            <NewLink pageId={pageId} onMade={setFresh} />
          )}
        </>
      )}
    </section>
  );
}

function NewLink({ pageId, onMade }: { pageId: string; onMade: (path: string) => void }) {
  const create = useCreatePageLink(pageId);
  const [label, setLabel] = useState("");
  const [days, setDays] = useState<number>(PUBLIC_LINK_DEFAULT_EXPIRY_DAYS);
  const fields = create.error instanceof ApiError ? create.error.fields : {};

  function submit(event: FormEvent) {
    event.preventDefault();
    const trimmed = label.trim();
    create.mutate(
      { ...(trimmed ? { label: trimmed } : {}), ...(days ? { expiresAt: linkExpiry(days) } : {}) },
      {
        onSuccess: (made) => {
          setLabel("");
          onMade(made.path);
        },
      },
    );
  }

  return (
    <form onSubmit={submit} noValidate className="space-y-3" aria-label={t.publicLinks.newTitle} data-public-link-form="">
      <div className="grid items-end gap-3 sm:grid-cols-[1fr_auto]">
        <Field
          label={t.publicLinks.label}
          placeholder={t.publicLinks.labelPlaceholder}
          value={label}
          maxLength={PUBLIC_LINK_LABEL_MAX_LENGTH}
          error={fields.label}
          className="w-full"
          onChange={(event) => {
            create.reset();
            setLabel(event.target.value);
          }}
        />
        <Select
          label={t.publicLinks.expires}
          value={days}
          error={fields.expiresAt}
          onChange={(event) => setDays(Number(event.target.value))}
          className="min-w-36"
          data-public-link-expiry=""
        >
          {PUBLIC_LINK_EXPIRY_DAYS.map((choice) => (
            <option key={choice} value={choice}>
              {t.publicLinks.inDays(choice)}
            </option>
          ))}
          <option value={0}>{t.publicLinks.never}</option>
        </Select>
      </div>
      {create.error && !fields.label && !fields.expiresAt && (
        <ErrorBanner>
          <span data-public-link-error="">{create.error.message}</span>
        </ErrorBanner>
      )}
      <Button type="submit" variant="secondary" icon={<Icon.Link />} loading={create.isPending} data-action="create-public-link">
        {t.publicLinks.create}
      </Button>
    </form>
  );
}

// The status line is always there, empty until needed, so a screen reader
// hears the copy succeed.
function FreshLink({ path }: { path: string }) {
  const address = new URL(path, window.location.origin).toString();
  const [said, setSaid] = useState("");
  const timer = useRef<ReturnType<typeof setTimeout> | undefined>(undefined);
  useEffect(() => () => clearTimeout(timer.current), []);

  async function copy() {
    let message = t.publicLinks.copied;
    try {
      await navigator.clipboard.writeText(address);
    } catch {
      message = t.publicLinks.copyFailed;
    }
    setSaid(message);
    clearTimeout(timer.current);
    timer.current = setTimeout(() => setSaid(""), COPY_FEEDBACK_MS);
  }

  return (
    <div className="space-y-2 rounded-control border border-accent/40 bg-surface-raised p-3" data-fresh-public-link="">
      <p className="text-sm text-ink">{t.publicLinks.freshBody}</p>
      <Field
        label={t.publicLinks.address}
        value={address}
        readOnly
        className="font-mono text-xs"
        onFocus={(event) => event.target.select()}
        data-public-link-address=""
      />
      <div className="flex flex-wrap items-center gap-2">
        <Button variant="secondary" size="sm" icon={<Icon.Copy />} onClick={copy} data-action="copy-public-link">
          {t.publicLinks.copy}
        </Button>
        <p role="status" className="text-xs text-ink-muted" data-copy-status="">
          {said}
        </p>
      </div>
    </div>
  );
}

function LinkList({ pageId, links }: { pageId: string; links: PublicLink[] }) {
  const { data: me } = useMe();
  const revoke = useRevokePageLink(pageId);
  return (
    <div className="space-y-2">
      <ul className="space-y-1.5" aria-label={t.publicLinks.listLabel} data-public-link-list="">
        {links.map((link) => {
          const label = link.label || t.publicLinks.unlabelled;
          const maker = !link.createdBy
            ? t.publicLinks.someoneGone
            : link.createdBy.id === me?.user.id
              ? t.publicLinks.you
              : link.createdBy.name || link.createdBy.email;
          return (
            <li
              key={link.id}
              className="flex min-h-9 items-center gap-2 rounded-control border border-border bg-surface-raised py-1 pr-1 pl-2 text-sm text-ink"
              data-public-link={link.label}
            >
              <Icon.Link className="shrink-0 text-ink-subtle" />
              <span className="min-w-0 flex-1">
                <span className="block font-medium">{label}</span>
                <span className="block text-xs text-ink-subtle" data-public-link-detail="">
                  {t.publicLinks.madeBy(maker, day.format(new Date(link.createdAt)))}{" "}
                  {link.expiresAt ? t.publicLinks.runsOut(day.format(new Date(link.expiresAt))) : t.publicLinks.neverRunsOut}
                </span>
              </span>
              <Button
                variant="ghost"
                size="sm"
                loading={revoke.isPending && revoke.variables === link.id}
                onClick={() => revoke.mutate(link.id)}
                aria-label={t.publicLinks.revokeNamed(label)}
                data-action="revoke-public-link"
              >
                {t.publicLinks.revoke}
              </Button>
            </li>
          );
        })}
      </ul>
      {revoke.error && <ErrorBanner>{revoke.error.message}</ErrorBanner>}
    </div>
  );
}
