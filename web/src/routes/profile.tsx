import { useEffect, useState } from "react";
import { createRoute, useRouter, useRouterState } from "@tanstack/react-router";
import { useMe, useSetLanguage } from "@/api/auth";
import { useSetShowInReaders } from "@/api/pageviews";
import { Avatar, Card, ErrorBanner, PageHeader, SectionTitle, Select, Switch } from "@/components/ui";
import { AccountSection } from "@/features/armature/AccountSection";
import { announceReturn, safeReturn } from "@/features/armature/connectLink";
import { ExciterAccountSection } from "@/features/exciter/AccountSection";
import {
  ARMATURE_OUTCOME_PARAM,
  ARMATURE_OUTCOMES,
  ARMATURE_RETURN_PARAM,
  ARMATURE_SECTION_ID,
  EXCITER_SECTION_ID,
  PROFILE_PATH,
  type ArmatureOutcome,
} from "@/config";
import { browserLanguage, isLanguage, LANGUAGES, resolveLanguage, t, useLanguage, type LanguageChoice } from "@/i18n";
import { appRoute } from "./app";

/** The caller's profile: who they are here, and their Armature and Exciter accounts. */
export const profileRoute = createRoute({
  getParentRoute: () => appRoute,
  path: PROFILE_PATH,
  validateSearch: (search: Record<string, unknown>): { [ARMATURE_RETURN_PARAM]?: string; [ARMATURE_OUTCOME_PARAM]?: ArmatureOutcome } => {
    const back = safeReturn(search[ARMATURE_RETURN_PARAM]);
    const outcome = ARMATURE_OUTCOMES.find((known) => known === search[ARMATURE_OUTCOME_PARAM]);
    return { ...(back ? { [ARMATURE_RETURN_PARAM]: back } : {}), ...(outcome ? { [ARMATURE_OUTCOME_PARAM]: outcome } : {}) };
  },
  component: ProfilePage,
});

function ProfilePage() {
  const { data: me } = useMe();
  const search = profileRoute.useSearch();
  const back = safeReturn(search[ARMATURE_RETURN_PARAM]);
  // The root route hands its search down unchecked, so this one is checked again where it is read, as the way back is.
  const outcome = ARMATURE_OUTCOMES.find((known) => known === search[ARMATURE_OUTCOME_PARAM]);
  const hash = useRouterState({ select: (s) => s.location.hash });
  const router = useRouter();
  const goBack = back
    ? () => {
        announceReturn(back);
        router.history.push(back);
      }
    : undefined;
  const goBackFromExciter = back
    ? () => {
        announceReturn(back, t.exciter.account.returned);
        router.history.push(back);
      }
    : undefined;
  // Back from Armature's consent page, connected: on to where the person was,
  // replacing the landing so the browser's back button skips it.
  useEffect(() => {
    if (outcome !== "connected" || !back) return;
    announceReturn(back);
    router.history.replace(back);
  }, [outcome, back, router]);
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
      {me && <PrivacySection shown={me.user.showInReaders} />}
      <AccountSection focusToken={hash === ARMATURE_SECTION_ID && !outcome} onReturn={goBack} returnTo={back} outcome={outcome} />
      <ExciterAccountSection focusToken={hash === EXCITER_SECTION_ID} onReturn={goBackFromExciter} />
    </div>
  );
}

function PrivacySection({ shown }: { shown: boolean }) {
  const save = useSetShowInReaders();
  return (
    <section aria-labelledby="profile-privacy" className="space-y-3" data-profile-privacy="">
      <SectionTitle id="profile-privacy">{t.profile.privacy}</SectionTitle>
      <Card className="space-y-3 p-4">
        <p className="text-sm text-ink-muted">{t.profile.privacyIntro}</p>
        {save.error && <ErrorBanner>{save.error.message}</ErrorBanner>}
        <div className="flex items-center gap-3">
          <Switch
            checked={shown}
            label={t.profile.showInReaders}
            disabled={save.isPending}
            onChange={(next) => save.mutate(next)}
            data-field="show-in-readers"
          />
          <span aria-hidden="true" className="text-sm font-medium text-ink">
            {t.profile.showInReaders}
          </span>
        </div>
        {!shown && <p className="text-sm text-ink-muted">{t.profile.hiddenNote}</p>}
        {save.isSuccess && (
          <p role="status" className="text-sm text-ink-muted" data-privacy-saved="">
            {t.profile.privacySaved}
          </p>
        )}
      </Card>
    </section>
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
