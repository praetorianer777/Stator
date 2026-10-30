import type { stewardshipEn } from "./stewardship.en";

/** The German strings of page owners, held to the English shape. */
export const stewardshipDe: typeof stewardshipEn = {
  verified: "Bestätigt",
  expired: "Bestätigung abgelaufen",
  badgeVerified: (until: string) => `Bestätigt bis ${until}. Verantwortliche Person und Bestätigung anzeigen.`,
  badgeExpired: (until: string) => `Bestätigung abgelaufen am ${until}. Verantwortliche Person und Bestätigung anzeigen.`,
  owner: (name: string) => `Verantwortlich: ${name}`,
  ownerNoAccess: "hat keinen Zugriff mehr",
  menu: "Verantwortung und Bestätigung",
  title: (page: string) => `Verantwortung und Bestätigung von ${page}`,
  ownerTitle: "Verantwortliche Person",
  ownerIntro: "Die Person, die für diese Seite einsteht. Sie wird erinnert, wenn die Bestätigung abläuft.",
  noOwner: "Für diese Seite ist noch niemand verantwortlich.",
  ownerLostAccess: (name: string) => `${name} kann diese Seite nicht mehr sehen und wird nicht erinnert. Wählen Sie jemanden, der sie sehen kann.`,
  pickOwner: "Verantwortliche Person wählen",
  removeOwner: "Verantwortliche Person entfernen",
  verificationTitle: "Bestätigung",
  verificationIntro:
    "Bestätigen Sie die Seite, sobald Sie geprüft haben, dass sie stimmt. Die Bestätigung läuft nach der gewählten Dauer ab, und die verantwortliche Person wird erinnert, die Seite erneut zu prüfen.",
  notVerified: "Niemand hat diese Seite bestätigt.",
  verifiedBy: (who: string, when: string, until: string) => `Bestätigt von ${who} am ${when}, gültig bis ${until}.`,
  expiredBy: (who: string, when: string, until: string) => `Bestätigt von ${who} am ${when}; die Bestätigung ist am ${until} abgelaufen.`,
  changedSince: (version: number, current: number) => `Geprüft wurde Version ${version}; die Seite ist jetzt bei Version ${current}.`,
  somebody: "jemandem",
  term: "Gültig für",
  days: (n: number) => `${n} Tage`,
  verify: "Bestätigen",
  verifyAgain: "Erneut bestätigen",
  unverify: "Bestätigung entfernen",
  readOnly: "Nur wer diese Seite bearbeiten darf, kann die verantwortliche Person ändern oder die Seite bestätigen.",
};
