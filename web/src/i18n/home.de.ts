import type { homeEn, exampleSpaceEn } from "./home.en";

/** The German strings of the home page and the example space, held to the English shape. */
export const homeDe: typeof homeEn = {
  title: "Start",
  emptyTitle: "Willkommen bei Stator",
  emptyBody: "Hier erscheinen die Seiten, die Sie schreiben und besuchen. Öffnen Sie zuerst die Bereiche, die Ihr Team teilt.",
  emptyAction: "Bereiche ansehen",
  starredTitle: "Mit Stern",
  starredEmpty: "Markieren Sie eine Seite oder einen Bereich oben mit dem Stern, um sie hier griffbereit zu haben.",
  recentTitle: "Zuletzt angesehen",
  recentEmpty: "Hier erscheinen die Seiten, die Sie öffnen.",
  editedTitle: "Zuletzt bearbeitet",
  editedEmpty: "Hier erscheinen die Seiten, die Sie veröffentlichen oder als Entwurf bearbeiten.",
  updatesTitle: "Neuigkeiten",
  updatesAll: "Alle Neuigkeiten",
  updatesWatched: "Beobachtet",
  updatesEmptyAll: "Noch hat niemand sonst eine Seite veröffentlicht, die Sie lesen können.",
  updatesEmptyWatched:
    "Was Sie beobachten, hat sich noch nicht geändert. Beobachten Sie eine Seite oder einen Bereich über die Kopfzeile, um sie hier zu verfolgen.",
  published: (who: string, version: number) => (version === 1 ? `${who} hat die Seite angelegt` : `${who} hat Version ${version} veröffentlicht`),
  somebody: "Jemand",
  more: "Mehr anzeigen",
  draft: "Entwurf",
  unpublished: "Unveröffentlicht",
  spaceNamed: (name: string) => `Bereich ${name}`,
  unstar: (what: string) => `Stern von ${what} entfernen`,
  unstarred: (what: string) => `${what} trägt keinen Stern mehr.`,
  loadFailed: "Diese Liste konnte nicht geladen werden. Versuchen Sie es gleich noch einmal.",
};

export const exampleSpaceDe: typeof exampleSpaceEn = {
  title: "Beispielbereich",
  create: "Beispielbereich anlegen",
  creating: "Beispielbereich wird angelegt",
  intro:
    "Ein Bereich, dessen Seiten Ihrem Team Stator erklären, mit einem Beispiel für jeden Block, den eine Seite enthalten kann. Er ist in der Sprache geschrieben, in der Sie Stator gerade lesen. Alle in der Organisation dürfen ihn lesen und kommentieren.",
  exists: "Den Beispielbereich gibt es schon:",
  archived: "Er ist archiviert. Holen Sie ihn in seinen Bereichseinstellungen aus dem Archiv, um ihn wieder zu ändern.",
  notAdmin: "Nur Administratoren der Organisation legen den Beispielbereich an. Bitten Sie eine dieser Personen, ihn anzulegen.",
  underWay: "Der Beispielbereich wird angelegt. Diese Seite öffnet ihn, sobald er fertig ist.",
  slow: "Mit dem Beispielbereich wurde noch nicht begonnen. Der Worker, der ihn anlegt, läuft vielleicht nicht; diese Seite prüft weiter, und wenn sich nichts ändert, bitten Sie die Person, die Stator betreibt, ihn zu starten.",
  gaveUp:
    "Der Beispielbereich ist nach einer Viertelstunde noch nicht angelegt. Laden Sie diese Seite neu, um nachzusehen, oder fragen Sie die Person, die Stator betreibt, ob ihr Worker läuft.",
  failures: {
    keys_taken:
      "Die Schlüssel STATOR bis STATOR9 gehören alle anderen Bereichen. Löschen Sie einen davon oder benennen Sie ihn um, und legen Sie den Beispielbereich dann noch einmal an.",
    forbidden: "Nur Administratoren der Organisation legen den Beispielbereich an. Bitten Sie eine dieser Personen, ihn anzulegen.",
    failed:
      "Der Beispielbereich konnte nicht angelegt werden, und was davon schon da war, wurde gelöscht. Legen Sie ihn noch einmal an; scheitert es wieder, bitten Sie die Person, die Stator betreibt, ins Protokoll des Workers zu sehen.",
    gone: "Der Beispielbereich wurde gelöscht, während er angelegt wurde. Legen Sie ihn noch einmal an.",
  },
  renewal: {
    available: "Ein neuerer Beispielbereich ist da. Er bringt:",
    intro:
      "Beim Erneuern werden seine Anleitungen auf den neuesten Stand gebracht. Seiten, die jemand geändert hat, bleiben, wie sie sind, und Kommentare, Sterne und Seiten, die andere angelegt haben, bleiben erhalten.",
    language: "Sprache der Seiten",
    languages: { en: "English", de: "Deutsch" },
    renew: "Beispielbereich erneuern",
    renewing: "Beispielbereich wird erneuert",
    underWay: "Der Beispielbereich wird erneuert. Das dauert ein paar Sekunden.",
    done: "Der Beispielbereich ist erneuert.",
    added: "Neue Seiten",
    remade: "Neu angelegt, weil sie gelöscht worden waren",
    kept: "Belassen, wie andere sie geändert haben",
    retired: "Nicht mehr Teil des Beispiels, unverändert gelassen",
    failures: {
      keys_taken:
        "Der Beispielbereich konnte nicht erneuert werden. Erneuern Sie ihn noch einmal; scheitert es wieder, bitten Sie die Person, die Stator betreibt, ins Protokoll des Workers zu sehen.",
      forbidden: "Nur Administratoren der Organisation erneuern den Beispielbereich. Bitten Sie eine dieser Personen, ihn zu erneuern.",
      failed:
        "Das Erneuern des Beispielbereichs ist unterwegs gescheitert. Die schon erneuerten Seiten bleiben erneuert und die anderen, wie sie waren; erneuern Sie ihn noch einmal, und scheitert es wieder, bitten Sie die Person, die Stator betreibt, ins Protokoll des Workers zu sehen.",
      gone: "Der Beispielbereich wurde gelöscht, bevor er erneuert wurde. Legen Sie den Beispielbereich neu an, um den aktuellen zu bekommen.",
    },
  },
};
