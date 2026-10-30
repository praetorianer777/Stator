import type { profileEn, languagesEn } from "./profile.en";

/** The German strings of the profile, held to the English shape. */
export const profileDe: typeof profileEn = {
  title: "Ihr Profil",
  account: "Konto",
  language: "Sprache und Zeitzone",
  languageIntro:
    "Die Sprache, in der Stator mit Ihnen spricht und in der es auch Datumsangaben und Zahlen schreibt, und die Zeitzone jeder Uhrzeit, die es zeigt.",
  languageField: "Sprache der Oberfläche",
  languageBrowser: (name: string) => `Wie der Browser (derzeit ${name})`,
  timezoneField: "Zeitzone",
  timezoneBrowser: (zone: string) => `Wie der Browser (derzeit ${zone})`,
  timezoneSearch: "Suchen, etwa Berlin",
  timezoneNone: "Keine Zeitzone hat das im Namen. Versuchen Sie eine Stadt oder eine Region.",
  preview: (when: string) => `Eine Uhrzeit sieht so aus: ${when}.`,
  save: "Speichern",
  languageSaved: "Ihre Sprache und Zeitzone sind gespeichert.",
  languageBoundary: "Nachrichten vom Server, etwa E-Mails und manche Fehlermeldungen, sind vorerst auf Englisch.",
  privacy: "Datenschutz",
  privacyIntro: "Wer eine Seite bearbeiten darf, sieht, wer sie zuletzt gelesen hat. Alle anderen, die sie lesen dürfen, sehen nur, wie oft sie gelesen wurde.",
  showInReaders: "Meinen Namen den Bearbeitenden der Seiten zeigen, die ich lese",
  hiddenNote: "Wenn Sie eine Seite lesen, werden Sie weiterhin gezählt, nur ohne Ihren Namen.",
  privacySaved: "Ihre Wahl ist gespeichert.",
};

/** Each language in its own words, so a reader finds theirs whatever is showing. */
export const languagesDe: typeof languagesEn = {
  en: "English",
  de: "Deutsch",
};
