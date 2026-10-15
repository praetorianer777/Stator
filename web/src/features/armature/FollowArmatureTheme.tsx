import { useArmatureAccount, useArmatureThemeFollow, useFollowArmatureTheme, useUnfollowArmatureTheme, type ArmatureThemeFollow } from "@/api/armature";
import { Card, ErrorBanner, Switch } from "@/components/ui";
import { t } from "@/i18n";
import { ConnectArmatureLink } from "./connectLink";
import { outdatedSentence } from "./outdated";

/** What the follow says to its owner: what they see, or why Armature's theme is not it. */
export function followState(follow: ArmatureThemeFollow | undefined): string {
  if (follow?.status === "outdated") return outdatedSentence("theme");
  if (!follow?.following) return t.themes.armature.off;
  if (follow.error) return follow.error;
  switch (follow.status) {
    case "ok":
      return t.themes.armature.on;
    case "rejected":
      return t.themes.armature.rejected;
    case "not_connected":
    case "not_configured":
      return t.themes.armature.notConnected;
    default:
      return t.themes.armature.unreachable;
  }
}

/** Follow my Armature theme, offered to whoever stored an Armature token; the others are shown how to store one. */
export function FollowArmatureTheme() {
  const { data: account } = useArmatureAccount();
  const connected = Boolean(account?.configured && account.connected);
  const { data: follow } = useArmatureThemeFollow(connected);
  const start = useFollowArmatureTheme();
  const stop = useUnfollowArmatureTheme();
  if (!connected) {
    if (!account?.configured) return null;
    return (
      <Card className="mb-4 p-4" data-armature-theme-connect="">
        <p className="text-sm text-ink-muted">
          {t.themes.armature.notConnected} <ConnectArmatureLink className="underline">{t.armature.list.connectLink}</ConnectArmatureLink>
        </p>
      </Card>
    );
  }

  const following = follow?.following ?? false;
  const failure = start.error ?? stop.error;
  const outdated = follow?.status === "outdated";
  const trouble = outdated || (following && (follow?.status !== "ok" || Boolean(follow?.error)));
  return (
    <Card className="mb-4 space-y-2 p-4" data-armature-theme={following ? "following" : "off"} data-armature-theme-status={follow?.status}>
      <div className="flex items-center gap-3">
        <Switch
          label={t.themes.armature.follow}
          checked={following}
          disabled={!follow || (outdated && !following) || start.isPending || stop.isPending}
          onChange={(on) => (on ? start.mutate() : stop.mutate())}
          data-action="follow-armature-theme"
        />
        <span aria-hidden="true" className="text-sm font-medium text-ink">
          {t.themes.armature.follow}
        </span>
      </div>
      <p role="status" className={trouble ? "text-sm text-ink" : "text-sm text-ink-muted"} data-armature-theme-note>
        {followState(follow)}
        {followState(follow) === t.themes.armature.rejected && (
          <>
            {" "}
            <ConnectArmatureLink className="underline">{t.armature.account.replace}</ConnectArmatureLink>
          </>
        )}
      </p>
      {failure && <ErrorBanner>{failure.message}</ErrorBanner>}
    </Card>
  );
}
