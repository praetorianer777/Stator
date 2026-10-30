import type { notificationsEn } from "./notifications.en";

/** The German strings of notifications, held to the English shape. */
export const notificationsDe: typeof notificationsEn = {
  bell: "Benachrichtigungen",
  bellUnread: (n: number) => (n === 1 ? "Benachrichtigungen, 1 ungelesen" : `Benachrichtigungen, ${n} ungelesen`),
  title: "Benachrichtigungen",
  markAllRead: "Alle als gelesen markieren",
  empty: "Nichts Neues. Hier erfahren Sie von Seiten, die Sie beobachten.",
  unread: "Ungelesen",
  failed: "Ihre Benachrichtigungen konnten nicht geladen werden. Versuchen Sie es gleich noch einmal.",
  settings: "Benachrichtigungseinstellungen",
  seeAll: "Alle Benachrichtigungen",
  unreadSaid: (n: number) => `${n} ungelesen`,
  page: {
    show: "Anzeigen",
    unread: "Ungelesen",
    everything: "Alle",
    nothingUnread: "Nichts ungelesen",
    nothingYet: "Noch keine Benachrichtigungen",
    emptyHint: "Hier erfahren Sie von den Seiten, die Sie beobachten, den Aufgaben, die Ihnen gegeben werden, und den Entwürfen, die Sie prüfen sollen.",
    pager: "Seiten der Benachrichtigungen",
    noun: { one: "Benachrichtigung", many: "Benachrichtigungen" },
  },
  sentence: (kind: string, actor: string, title: string, version: number | null, inComment = false) => {
    const who = actor || "Jemand";
    switch (kind) {
      case "assigned":
        return `${who} hat Ihnen eine Aufgabe auf ${title} zugewiesen`;
      case "due":
        return `Eine Ihrer Aufgaben auf ${title} ist fällig`;
      case "review":
        return `${who} bittet Sie, einen Entwurf von ${title} zu prüfen`;
      case "reviewed":
        return `${who} hat über Ihren Entwurf von ${title} entschieden`;
      case "mentioned":
        return inComment ? `${who} hat Sie in einem Kommentar zu ${title} erwähnt` : `${who} hat Sie auf ${title} erwähnt`;
      case "shared":
        return `${who} hat ${title} mit Ihnen geteilt`;
      case "replied":
        return `${who} hat in einer Diskussion zu ${title} geantwortet`;
      case "commented":
        return `${who} hat ${title} kommentiert`;
      case "resolved":
        return `${who} hat eine Diskussion zu ${title} als erledigt markiert oder wieder geöffnet`;
      case "published":
        return version ? `${who} hat Version ${version} von ${title} veröffentlicht` : `${who} hat ${title} veröffentlicht`;
      case "created":
        return `${who} hat ${title} angelegt`;
      case "posted":
        return `${who} hat den Beitrag ${title} veröffentlicht`;
      case "expired":
        return `Die Bestätigung von ${title} ist abgelaufen. Prüfen Sie die Seite und bestätigen Sie sie erneut.`;
      case "failed":
        return `Ihre geplante Veröffentlichung von ${title} ging nicht hinaus. Öffnen Sie die Seite, um zu sehen, warum.`;
      default:
        return `${who} hat ${title} geändert`;
    }
  },
  preferencesTitle: "Benachrichtigungen",
  preferencesIntro:
    "Wählen Sie, wovon Sie erfahren, in der App und per E-Mail. Eine E-Mail ist eine Kopie dessen, was die App zeigt; eine Art, die in der App ausgeschaltet ist, verschickt also auch keine E-Mail.",
  kindColumn: "Wenn",
  inApp: "In der App",
  email: "Per E-Mail",
  kinds: {
    assigned: "Jemand weist Ihnen eine Aufgabe zu",
    due: "Eine Ihnen zugewiesene Aufgabe wird fällig",
    review: "Jemand bittet Sie, einen Entwurf zu prüfen",
    reviewed: "Jemand entscheidet über Ihren Entwurf",
    mentioned: "Jemand erwähnt Sie",
    shared: "Jemand teilt eine Seite mit Ihnen",
    replied: "Jemand antwortet in einer Diskussion, in der Sie geschrieben haben",
    commented: "Jemand kommentiert eine Seite, die Sie beobachten",
    resolved: "Eine Diskussion, in der Sie geschrieben haben, wird als erledigt markiert oder wieder geöffnet",
    published: "Eine Seite, die Sie beobachten, wird mit Benachrichtigung veröffentlicht",
    created: "Unter einer Seite oder einem Bereich, die Sie beobachten, wird erstmals eine Seite veröffentlicht",
    posted: "In einem Blog oder Bereich, den Sie beobachten, erscheint ein Beitrag",
    expired: "Die Bestätigung einer Seite, die Ihnen gehört, läuft ab",
    failed: "Eine von Ihnen geplante Veröffentlichung kann nicht hinausgehen",
  } as Record<
    | "assigned"
    | "due"
    | "review"
    | "reviewed"
    | "mentioned"
    | "shared"
    | "replied"
    | "commented"
    | "resolved"
    | "published"
    | "created"
    | "posted"
    | "expired"
    | "failed",
    string
  >,
  inAppFor: (kind: string) => `In der App: ${kind}`,
  emailFor: (kind: string) => `Per E-Mail: ${kind}`,
  digest: "Wann E-Mails verschickt werden",
  digests: {
    off: "Eine E-Mail je Benachrichtigung, sofort",
    hourly: "Eine E-Mail pro Stunde mit allem Neuen",
    daily: (hour: number) => `Eine E-Mail am Tag um ${String(hour).padStart(2, "0")}:00 Uhr UTC mit allem Neuen`,
  },
  digestHint: "Eine Sammel-E-Mail lässt weg, was Sie in der App schon gelesen haben.",
  autoWatch: "Seiten beobachten, die ich anlege und veröffentliche",
  autoWatchHint: "Wenn Sie eine Seite nicht mehr beobachten, beobachten Sie sie durch eigene Änderungen nicht wieder.",
  save: "Speichern",
  saved: "Ihre Benachrichtigungseinstellungen sind gespeichert.",
};
