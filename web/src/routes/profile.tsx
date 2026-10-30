import { useEffect, useState, type FormEvent } from "react";
import { createRoute, useRouter, useRouterState } from "@tanstack/react-router";
import { useMe, useSaveReading } from "@/api/auth";
import { useSetShowInReaders } from "@/api/pageviews";
import { Avatar, Button, Card, ErrorBanner, Labelled, PageHeader, SearchPicker, SectionTitle, Select, Switch } from "@/components/ui";
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
  TIME_ZONE_MATCHES_MAX,
  type ArmatureOutcome,
} from "@/config";
import { browserLanguage, isLanguage, LANGUAGES, resolveLanguage, t, useLanguage, type LanguageChoice } from "@/i18n";
import { browserTimeZone, isTimeZone, timeZone, timeZones } from "@/lib/format";
import { appRoute } from "./app";
import { settingsCrumb } from "@/features/settings/SettingsHome";

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
      <PageHeader crumbs={[settingsCrumb()]} title={t.profile.title} />
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
      {me && <ReadingSection language={me.user.locale} zone={me.user.timezone} />}
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

/** A zone as a person reads it: America/New York rather than America/New_York. */
function zoneName(zone: string): string {
  return zone.replace(/_/g, " ");
}

// The zones whose name holds what was typed, a few at most, with the chosen one
// among them so the select can show it; nothing typed lists only the choice.
function zoneMatches(words: string, chosen: string): string[] {
  const wanted = words.trim().toLowerCase().replace(/_/g, " ");
  const found = wanted
    ? timeZones()
        .filter((zone) => zoneName(zone).toLowerCase().includes(wanted))
        .slice(0, TIME_ZONE_MATCHES_MAX)
    : [];
  return chosen && !found.includes(chosen) ? [chosen, ...found] : found;
}

// A new language or zone draws the page again from scratch, so the notice that
// the choice was saved has to outlive the component that made it.
let savedNotice = false;

/** The language and the time zone, chosen together and kept by one Save. */
function ReadingSection({ language, zone }: { language: LanguageChoice; zone: string }) {
  const active = useLanguage();
  const save = useSaveReading();
  const [choice, setChoice] = useState<LanguageChoice>(language);
  const [chosenZone, setChosenZone] = useState(zone);
  const [zoneWords, setZoneWords] = useState("");
  const [saved, setSaved] = useState(savedNotice);
  useEffect(() => {
    savedNotice = false;
  }, []);
  const zones = zoneMatches(zoneWords, chosenZone);
  const previewZone = isTimeZone(chosenZone) ? chosenZone : browserTimeZone();
  const preview = new Intl.DateTimeFormat(resolveLanguage(choice), { dateStyle: "medium", timeStyle: "short", timeZone: previewZone }).format(new Date());

  function submit(event: FormEvent) {
    event.preventDefault();
    setSaved(false);
    savedNotice = resolveLanguage(choice) !== active || (isTimeZone(chosenZone) ? chosenZone : browserTimeZone()) !== timeZone();
    save.mutate(
      { locale: choice, timezone: chosenZone },
      {
        onSuccess: () => setSaved(true),
        onError: () => {
          savedNotice = false;
        },
      },
    );
  }

  return (
    <section aria-labelledby="profile-language" className="space-y-3" data-profile-language="">
      <SectionTitle id="profile-language">{t.profile.language}</SectionTitle>
      <Card className="p-4">
        <form onSubmit={submit} className="space-y-3" noValidate>
          <p className="text-sm text-ink-muted">{t.profile.languageIntro}</p>
          {save.error && <ErrorBanner>{save.error.message}</ErrorBanner>}
          <Select
            id="field-language"
            label={t.profile.languageField}
            value={choice}
            disabled={save.isPending}
            onChange={(event) => {
              setSaved(false);
              setChoice(isLanguage(event.target.value) ? event.target.value : "");
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
          <Labelled id="field-timezone" label={t.profile.timezoneField}>
            <SearchPicker
              id="field-timezone"
              aria-label={t.profile.timezoneField}
              value={chosenZone}
              onChange={(zone) => {
                setSaved(false);
                setChosenZone(zone);
              }}
              words={zoneWords}
              onWords={setZoneWords}
              options={zones.map((each) => ({ value: each, label: zoneName(each) }))}
              leading={<option value="">{t.profile.timezoneBrowser(zoneName(browserTimeZone()))}</option>}
              placeholder={t.profile.timezoneSearch}
              nothing={zoneWords.trim() && zones.every((each) => each === chosenZone) ? t.profile.timezoneNone : undefined}
              disabled={save.isPending}
              controlSize="md"
            />
          </Labelled>
          <p className="text-sm text-ink-muted" data-format-preview="">
            {t.profile.preview(preview)}
          </p>
          {active !== "en" && <p className="text-sm text-ink-subtle">{t.profile.languageBoundary}</p>}
          <div className="flex flex-wrap items-center gap-3">
            <Button type="submit" loading={save.isPending} data-action="save-reading">
              {t.profile.save}
            </Button>
            {saved && (
              <p role="status" className="text-sm text-ink-muted" data-language-saved="">
                {t.profile.languageSaved}
              </p>
            )}
          </div>
        </form>
      </Card>
    </section>
  );
}
