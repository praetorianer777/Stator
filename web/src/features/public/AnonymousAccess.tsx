import { useState } from "react";
import { useMe } from "@/api/auth";
import {
  useAnonymousAccess,
  usePublicLinkSettings,
  useSetAnonymousAccess,
  useSetPublicLinkSettings,
  useSetSpaceAnonymousAccess,
  useSpaceAnonymousAccess,
  type AnonymousAccessSettings,
} from "@/api/public";
import type { Space } from "@/api/spaces";
import { Card, ErrorBanner, Skeleton, Switch } from "@/components/ui";
import { publicSitePath, publicSpacePath } from "@/features/editor/publicReading";
import { t } from "@/i18n";

/** An address of this site as somebody would type it, for the public pages. */
function absolute(path: string): string {
  return new URL(path, window.location.origin).toString();
}

/** The organization's switch for reading without signing in, and whether search engines are asked in; each change is saved at once. */
export function OrgAnonymousAccess() {
  const { data: me } = useMe();
  const { data, error, refetch } = useAnonymousAccess();
  const save = useSetAnonymousAccess();
  const [notice, setNotice] = useState("");
  const slug = me?.organization?.slug;

  function change(next: AnonymousAccessSettings) {
    setNotice("");
    save.mutate(next, { onSuccess: () => setNotice(t.anonymousAccess.saved) });
  }

  return (
    <Card className="space-y-3 p-4" data-anonymous-access="">
      <h2 className="font-semibold text-ink">{t.anonymousAccess.title}</h2>
      <p className="text-sm text-ink-muted">{t.anonymousAccess.intro}</p>
      {error && <ErrorBanner onRetry={() => void refetch()}>{error.message}</ErrorBanner>}
      {!data && !error && <Skeleton />}
      {data && (
        <div className="space-y-3">
          <span className="flex items-center gap-3 text-sm text-ink">
            <Switch
              checked={data.enabled}
              label={t.anonymousAccess.enabled}
              disabled={save.isPending}
              onChange={(enabled) => change({ ...data, enabled })}
              data-action="anonymous-access"
            />
            <span aria-hidden="true">{t.anonymousAccess.enabled}</span>
          </span>
          <span className="flex items-center gap-3 text-sm text-ink">
            <Switch
              checked={data.indexable}
              label={t.anonymousAccess.indexable}
              disabled={save.isPending || !data.enabled}
              onChange={(indexable) => change({ ...data, indexable })}
              data-action="anonymous-indexable"
            />
            <span aria-hidden="true">{t.anonymousAccess.indexable}</span>
          </span>
          {data.enabled && slug && (
            <p className="text-sm text-ink-muted" data-public-address="">
              {t.anonymousAccess.address(absolute(publicSitePath(slug)))}
            </p>
          )}
        </div>
      )}
      {save.error && <ErrorBanner>{save.error.message}</ErrorBanner>}
      <span role="status" className="text-sm text-ink-muted">
        {notice}
      </span>
    </Card>
  );
}

/** The organization's switch for public links to single pages, apart from its open spaces; saved at once. */
export function OrgPublicLinks() {
  const { data, error, refetch } = usePublicLinkSettings();
  const save = useSetPublicLinkSettings();
  const [notice, setNotice] = useState("");
  return (
    <Card className="space-y-3 p-4" data-public-links-settings="">
      <h2 className="font-semibold text-ink">{t.publicLinks.settingsTitle}</h2>
      <p className="text-sm text-ink-muted">{t.publicLinks.settingsIntro}</p>
      {error && <ErrorBanner onRetry={() => void refetch()}>{error.message}</ErrorBanner>}
      {!data && !error && <Skeleton />}
      {data && (
        <span className="flex items-center gap-3 text-sm text-ink">
          <Switch
            checked={data.enabled}
            label={t.publicLinks.settingsToggle}
            disabled={save.isPending}
            onChange={(enabled) => {
              setNotice("");
              save.mutate({ enabled }, { onSuccess: () => setNotice(t.publicLinks.saved) });
            }}
            data-action="public-links"
          />
          <span aria-hidden="true">{t.publicLinks.settingsToggle}</span>
        </span>
      )}
      {save.error && <ErrorBanner>{save.error.message}</ErrorBanner>}
      <span role="status" className="text-sm text-ink-muted">
        {notice}
      </span>
    </Card>
  );
}

/** Whether anybody may read a space without signing in, for its administrators; saved at once. */
export function SpaceAnonymousAccess({ space }: { space: Space }) {
  const { data: me } = useMe();
  const allowed = space.can.administer;
  const { data, error, refetch } = useSpaceAnonymousAccess(space.key, allowed);
  const save = useSetSpaceAnonymousAccess(space.key);
  const slug = me?.organization?.slug;
  if (!allowed) return null;
  return (
    <section className="space-y-3 border-t border-border pt-4" data-space-anonymous-access="">
      <h2 className="font-semibold text-ink">{t.anonymousAccess.spaceTitle}</h2>
      <p className="text-sm text-ink-muted">{t.anonymousAccess.spaceIntro}</p>
      {error && <ErrorBanner onRetry={() => void refetch()}>{error.message}</ErrorBanner>}
      {!data && !error && <Skeleton />}
      {data &&
        (data.personal ? (
          <p className="text-sm text-ink-muted">{t.anonymousAccess.personal}</p>
        ) : (
          <>
            <span className="flex items-center gap-3 text-sm text-ink">
              <Switch
                checked={data.view}
                label={t.anonymousAccess.spaceToggle}
                disabled={save.isPending}
                onChange={(view) => save.mutate(view)}
                data-action="space-anonymous-access"
              />
              <span aria-hidden="true">{t.anonymousAccess.spaceToggle}</span>
            </span>
            {data.view && !data.orgEnabled && <p className="text-sm text-warning">{t.anonymousAccess.orgOff}</p>}
            {data.view && data.orgEnabled && slug && (
              <p className="text-sm text-ink-muted" data-space-public-address="">
                {t.anonymousAccess.spaceAddress(absolute(publicSpacePath(slug, space.key)))}
              </p>
            )}
          </>
        ))}
      {save.error && <ErrorBanner>{save.error.message}</ErrorBanner>}
    </section>
  );
}
