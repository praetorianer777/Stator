/** The English strings of the profile. */
export const profileEn = {
  title: "Your profile",
  account: "Account",
  language: "Language and time zone",
  languageIntro: "The language Stator speaks to you, which dates and numbers are written in too, and the time zone of every time it shows.",
  languageField: "Interface language",
  languageBrowser: (name: string) => `Follow the browser (now ${name})`,
  timezoneField: "Time zone",
  timezoneBrowser: (zone: string) => `Follow the browser (now ${zone})`,
  timezoneSearch: "Search, such as Berlin",
  timezoneNone: "No time zone has that in its name. Try a city or a region.",
  preview: (when: string) => `A time reads like this: ${when}.`,
  save: "Save",
  languageSaved: "Your language and time zone are saved.",
  languageBoundary: "Messages from the server, such as emails and some error messages, are in English for now.",
  privacy: "Privacy",
  privacyIntro: "People who may edit a page see who read it lately. Everybody else who may read it sees only how often it was read.",
  showInReaders: "Show my name to the editors of pages I read",
  hiddenNote: "You are still counted when you read a page, only without your name.",
  privacySaved: "Your choice is saved.",
};

/** Each language in its own words, so a reader finds theirs whatever is showing. */
export const languagesEn = {
  en: "English",
  de: "Deutsch",
};
