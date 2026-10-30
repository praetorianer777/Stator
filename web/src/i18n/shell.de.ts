import type { navEn, shellEn, themeEn, accountEn, viaEn, notFoundEn, routeErrorEn, apiEn } from "./shell.en";

/** The German strings of the shell: the rail, the sidebar, the account menu and the pages that are not there, held to the English shape. */
export const navDe: typeof navEn = {
  hub: "Hub",
  everywhere: "Überall",
  whereYouAre: "Hier",
  home: "Start",
  spaces: "Bereiche",
  search: "Suche",
  tasks: "Meine Aufgaben",
  reviews: "Prüfungen",
  notifications: "Benachrichtigungen",
  groupWiki: "Wiki",
  collapseSidebar: "Seitenleiste einklappen",
  expandSidebar: "Seitenleiste ausklappen",
  drawer: "Navigation",
  openDrawer: "Navigation öffnen",
  buildVersion: (version: string) => `Stator ${version}`,
  buildCommit: (commit: string) => `Commit ${commit}`,
  buildCommitUnknown: "Commit unbekannt",
  buildBuiltAt: (when: string) => `Gebaut am ${when}`,
  closeDrawer: "Navigation schließen",
};

export const shellDe: typeof shellEn = {
  skipToContent: "Zum Inhalt springen",
};

export const themeDe: typeof themeEn = {
  system: "Automatisch",
  light: "Hell",
  dark: "Dunkel",
  tooltip: (label: string) => `Darstellung: ${label}`,
};

export const accountDe: typeof accountEn = {
  menu: "Ihr Konto",
  guest: "Gast",
  profile: "Ihr Profil",
  themes: "Designs",
  tokens: "Zugriffstokens",
  notifications: "Benachrichtigungseinstellungen",
  watching: "Beobachtet",
  signOut: "Abmelden",
  switchTo: "Wechseln zu",
  armatureAccount: "Armature-Konto",
  exciterAccount: "Exciter-Konto",
};

export const viaDe: typeof viaEn = {
  through: (name: string) => `über ${name}`,
  kinds: { token: "ein persönliches Zugriffstoken", oauth: "eine verbundene App" } as Record<"token" | "oauth", string>,
};

export const notFoundDe: typeof notFoundEn = {
  title: "Seite nicht gefunden",
  emptyTitle: "Unter dieser Adresse gibt es nichts",
  body: "Die gewünschte Seite gibt es nicht. Prüfen Sie die Adresse, oder gehen Sie zum Start.",
  action: "Zum Start",
};

export const routeErrorDe: typeof routeErrorEn = {
  title: "Diese Seite konnte nicht angezeigt werden",
  emptyTitle: "Etwas ist schiefgegangen",
  retry: "Erneut versuchen",
  fallback: "Beim Laden dieser Seite ist etwas schiefgegangen. Versuchen Sie es noch einmal, und laden Sie neu, wenn es weiter passiert.",
};

export const apiDe: typeof apiEn = {
  unexpected: (status: number) => `Der Server hat mit Status ${status} geantwortet. Versuchen Sie es gleich noch einmal.`,
  unsaved: "Diese Änderung konnte nicht gespeichert werden. Prüfen Sie die Verbindung, und versuchen Sie es noch einmal.",
};
