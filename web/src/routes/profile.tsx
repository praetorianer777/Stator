import { useEffect, useState } from "react";
import { createRoute } from "@tanstack/react-router";
import { useMe, useSetLanguage } from "@/api/auth";
import { Avatar, Card, ErrorBanner, PageHeader, SectionTitle, Select } from "@/components/ui";
import { AccountSection } from "@/features/armature/AccountSection";
import { PROFILE_PATH } from "@/config";
import { browserLanguage, isLanguage, LANGUAGES, resolveLanguage, t, useLanguage, type LanguageChoice } from "@/i18n";
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
      {me && <LanguageSection choice={me.user.locale} />}
      <AccountSection />
    </div>
  );
}

// A new language draws the page again from scratch, so the notice that the
// choice was saved has to outlive the component that made it.
let savedNotice = false;

function LanguageSection({ choice }: { choice: LanguageChoice }) {
  const active = useLanguage();
  const save = useSetLanguage();
  const [saved, setSaved] = useState(savedNotice);
  useEffect(() => {
    savedNotice = false;
  }, []);
  return (
    <section aria-labelledby="profile-language" className="space-y-3" data-profile-language="">
      <SectionTitle id="profile-language">{t.profile.language}</SectionTitle>
      <Card className="space-y-3 p-4">
        <p className="text-sm text-ink-muted">{t.profile.languageIntro}</p>
        {save.error && <ErrorBanner>{save.error.message}</ErrorBanner>}
        <Select
          id="field-language"
          label={t.profile.languageField}
          value={choice}
          disabled={save.isPending}
          onChange={(event) => {
            const next: LanguageChoice = isLanguage(event.target.value) ? event.target.value : "";
            setSaved(false);
            savedNotice = resolveLanguage(next) !== active;
            save.mutate(next, {
              onSuccess: () => setSaved(true),
              onError: () => {
                savedNotice = false;
              },
            });
          }}
          data-field="language"
        >
          <option value="">{t.profile.languageBrowser(t.languages[browserLanguage()])}</option>
          {LANGUAGES.map((each) => (
            <option key={each} value={each} lang={each}>
              {t.languages[each]}
            </option>
          ))}
        </Select>
        {active !== "en" && <p className="text-sm text-ink-subtle">{t.profile.languageBoundary}</p>}
        {saved && (
          <p role="status" className="text-sm text-ink-muted" data-language-saved="">
            {t.profile.languageSaved}
          </p>
        )}
      </Card>
    </section>
  );
}
