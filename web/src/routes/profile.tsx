import { createRoute } from "@tanstack/react-router";
import { useMe } from "@/api/auth";
import { Avatar, Card, PageHeader, SectionTitle } from "@/components/ui";
import { AccountSection } from "@/features/armature/AccountSection";
import { PROFILE_PATH } from "@/config";
import { t } from "@/i18n";
import { appRoute } from "./app";

/** The caller's profile: who they are here, and their Armature account. */
export const profileRoute = createRoute({ getParentRoute: () => appRoute, path: PROFILE_PATH, component: ProfilePage });

function ProfilePage() {
  const { data: me } = useMe();
  return (
    <div className="mx-auto max-w-3xl space-y-6">
      <PageHeader crumb={t.settings.title} title={t.profile.title} />
      {me && (
        <section aria-labelledby="profile-account" className="space-y-3">
          <SectionTitle id="profile-account">{t.profile.account}</SectionTitle>
          <Card className="flex items-center gap-3 p-4">
            <Avatar name={me.user.name} src={me.user.avatarUrl} size="md" />
            <div className="min-w-0 text-sm">
              <p className="truncate font-medium text-ink">{me.user.name}</p>
              <p className="truncate text-ink-muted">{me.user.email}</p>
            </div>
          </Card>
        </section>
      )}
      <AccountSection />
    </div>
  );
}
