import type { siteTransferEn, spaceTransferEn } from "./siteTransfer.en";

/** The German strings of exporting and importing a site or a space, held to the English shape. */
export const siteTransferDe: typeof siteTransferEn = {
  title: "Export und Import",
  notAdmin: "Nur Administratoren der Organisation exportieren oder importieren sie ganz. Bitten Sie eine von ihnen darum.",
  exportTitle: "Die Organisation exportieren",
  exportIntro:
    "Ein Archiv der ganzen Organisation, das im Hintergrund entsteht: jeder Bereich mit seinen Seiten, dem Verlauf, den Dateien, Kommentaren und Entwürfen, dazu die Personen, Gruppen, Berechtigungen, Einstellungen, die Marke, Designs, Vorlagen, Webhooks und das Audit-Log. Importieren Sie es in ein frisches Stator, um die Organisation dorthin umzuziehen. Die Datei liegt einen Tag lang zum Herunterladen bereit.",
  export: "Organisation exportieren",
  exporting: "Die Organisation wird exportiert",
  archive: "Archiv der Organisation",
  noExports: "Die Organisation wurde noch nicht exportiert.",
  importTitle: "Eine Organisation importieren",
  importIntro:
    "Füllen Sie diese Organisation aus dem Archiv einer ganzen Organisation, das ein anderes Stator erzeugt hat. Ihre Bereiche, Personen, Gruppen und Einstellungen kommen herein; Personen werden an ihrer E-Mail-Adresse erkannt, und alle melden sich hier neu an. Das geht nur, solange diese Organisation keine Bereiche hat.",
  importSite: "Organisation importieren",
  imported: "Die Organisation ist importiert. Alle melden sich hier neu an.",
  importedBy: (name: string, when: string) => `Importiert von ${name} am ${when}`,
  notCarriedTitle: "Was nicht mitkommt",
  notCarriedHint: "Das Archiv führt dies ebenfalls auf. Vergeben Sie es dort neu, wo die Organisation ankommt.",
  notCarried: {
    passwords: {
      what: "Passwörter",
      why: "Nur eine Bootstrap-Administratorin hat eines, aus den Einstellungen des Stator, in dem die Organisation ankommt.",
    },
    sessions: { what: "Sitzungen", why: "Alle melden sich dort neu an, wo die Organisation ankommt; Konten werden an ihrer Adresse erkannt." },
    tokens: { what: "Persönliche Zugriffstokens", why: "Ihre Geheimnisse werden nicht lesbar aufbewahrt. Ihre Inhaber erstellen neue." },
    armatureAccounts: { what: "Verbundene Armature-Konten", why: "Alle verbinden ihr Armature-Konto in ihrem Profil neu." },
    signInSecret: {
      what: "Das Client-Geheimnis des Anmeldeanbieters",
      why: "Der Anbieter kommt ausgeschaltet an, sofern die Organisation, in der er ankommt, keinen eigenen hat; tragen Sie das Geheimnis ein und schalten Sie ihn ein.",
    },
    webhookSecrets: {
      what: "Signaturgeheimnisse der Webhooks",
      why: "Jeder Webhook kommt ausgeschaltet an; erneuern Sie sein Geheimnis, geben Sie es dem Empfänger und schalten Sie ihn ein.",
    },
    armatureWebhookSecret: {
      what: "Das Webhook-Geheimnis von Armature",
      why: "Speichern Sie die Armature-Verbindung neu und tragen Sie das neue Geheimnis in Armature ein.",
    },
    exciterAccounts: { what: "Verbundene Exciter-Konten", why: "Alle fügen in ihrem Profil neu ein Exciter-Token ein." },
    exciterWebhookSecret: {
      what: "Das Webhook-Geheimnis von Exciter",
      why: "Speichern Sie die Exciter-Verbindung neu und tragen Sie das neue Geheimnis in Exciter ein.",
    },
    joinRequests: { what: "Beitrittsanfragen", why: "Wer weiterhin hinein möchte, meldet sich neu an und fragt erneut." },
    links: {
      what: "Freigaben, öffentliche Links und anonymes Lesen über Links",
      why: "Ein Link öffnet die Organisation für Personen; vergeben Sie ihn dort neu, wo sie ankommt.",
    },
    personal: { what: "Benachrichtigungen, Sterne, Beobachtungen, Seitenaufrufe und Besuche", why: "Sie sind das eigene Lesen jeder Person am alten Ort." },
    arrangements: {
      what: "Verantwortliche, Bestätigungen, geplante Veröffentlichungen, Verknüpfungen, Prüfungen und Lesebestätigungen von Seiten",
      why: "Sie sind Absprachen statt Inhalt, wie im Archiv eines Bereichs.",
    },
    sharedDrafts: {
      what: "Der gemeinsame Entwurf einer Seite, während mehrere sie bearbeiten",
      why: "Der eigene Entwurf jeder Person kommt mit, aus ihm gespeichert.",
    },
    trash: { what: "Der Papierkorb", why: "Was gelöscht wurde, bleibt gelöscht." },
    jobs: {
      what: "Aufträge und ihre Dateien",
      why: "Exporte, Importe, Word-Importe und die Aufträge des Beispielbereichs gehören zum Worker des alten Ortes.",
    },
    armatureThemes: {
      what: "Aus Armature gespiegelte Designs",
      why: "Sie kommen aus Armature zurück, wenn ihre Inhaber wieder dem Design von Armature folgen.",
    },
    address: { what: "Die Adresse der Organisation", why: "Die Organisation behält die Adresse des Stator, in dem sie ankommt." },
  } as Record<string, { what: string; why: string }>,
  report: {
    title: "Was übernommen wurde",
    spaces: "Bereiche",
    pages: "Seiten",
    versions: "Versionen",
    files: "Dateien",
    comments: "Kommentare",
    drafts: "Entwürfe",
    people: "Personen",
    matched: "Davon mit einem Konto hier",
    groups: "Gruppen",
    templates: "Vorlagen",
    themes: "Designs",
    webhooks: "Webhooks",
    auditEntries: "Einträge des Audit-Logs",
    kept: {
      signIn: "Diese Organisation hat ihren eigenen Anmeldeanbieter behalten.",
      armature: "Diese Organisation hat ihre eigene Armature-Verbindung behalten.",
      exciter: "Diese Organisation hat ihre eigene Exciter-Verbindung behalten.",
    } as Record<string, string>,
    webhooksOff: "Webhooks kamen ausgeschaltet an. Erneuern Sie das Geheimnis jedes Webhooks, geben Sie es dem Empfänger und schalten Sie ihn ein.",
  },
};

export const spaceTransferDe: typeof spaceTransferEn = {
  notAdmin: "Nur Administratoren dieses Bereichs können ihn exportieren. Fragen Sie eine von ihnen.",
  notCreator:
    "Nur wer Bereiche anlegen darf, kann einen importieren. Bitten Sie eine Administratorin der Organisation, es Ihnen zu erlauben oder den Bereich für Sie zu importieren.",
  exportIntro:
    "Ein Export ist eine Datei des ganzen Bereichs, die im Hintergrund entsteht. Jede Seite, die Leser lesen, ist mit Verlauf, Dateien, Stichwörtern, Kommentaren und Berechtigungen darin; Entwürfe nicht. Die Datei liegt einen Tag lang zum Herunterladen bereit.",
  format: "Format",
  formats: {
    archive: {
      title: "Archiv",
      description: "Alles im Bereich, mit jeder Version und Datei, um es hier oder in einer anderen Organisation wieder zu importieren.",
    },
    html: {
      title: "HTML-Seiten",
      description: "Die veröffentlichten Seiten als Webseiten mit ihren Bildern und Dateien, offline in jedem Browser lesbar.",
    },
  },
  export: "Bereich exportieren",
  exporting: "Der Bereich wird exportiert",
  recent: "Letzte Exporte",
  none: "Dieser Bereich wurde noch nicht exportiert.",
  askedBy: (name: string, when: string) => `Angefordert von ${name} am ${when}`,
  queued: "Wartet darauf, dass der Worker beginnt.",
  importQueued: "Die Datei ist hochgeladen und wartet darauf, dass der Worker beginnt. Diese Seite verfolgt sie.",
  progress: (done: number, total: number) => `${done} von ${total} Seiten und Dateien erledigt.`,
  progressLabel: (percent: number) => `${percent} Prozent erledigt`,
  expires: (when: string, size: string) => `Fertig, ${size}. Die Datei wird am ${when} gelöscht.`,
  expired: "Die Datei wurde nach ihrer Frist gelöscht. Exportieren Sie den Bereich erneut, um sie herunterzuladen.",
  download: "Herunterladen",
  importSpace: "Bereich importieren",
  importTitle: "Einen Bereich importieren",
  importIntro:
    "Wählen Sie ein Archiv, das ein Bereichsexport von Stator erzeugt hat, hier oder in einer anderen Organisation, oder den HTML- oder XML-Bereichsexport eines anderen Wikis als Zip. Es wird ein neuer Bereich mit seinen Seiten, dem Verlauf, den Dateien und Kommentaren; Personen werden an ihrer E-Mail-Adresse erkannt und Gruppen an ihrem Namen.",
  chooseArchive: "Datei wählen",
  noArchive: "Noch keine Datei gewählt.",
  keyHint:
    "Leer lassen, um den Schlüssel des Archivs zu behalten; der Export eines anderen Wikis braucht einen. Er wird abgelehnt, wenn ein anderer Bereich ihn hat.",
  nameHint: "Leer lassen, um den Namen zu behalten, den das Archiv oder der Export nennt.",
  sending: (percent: number) => `Die Datei wird hochgeladen, ${percent} Prozent`,
  uploadBroke: "Das Hochladen brach ab. Prüfen Sie Ihre Verbindung und importieren Sie die Datei erneut.",
  tryAgain: "Eine andere Datei importieren",
  imported: "Der Bereich ist importiert.",
  open: (key: string) => `${key} öffnen`,
  report: {
    counts: (pages: number, versions: number, files: number, comments: number) =>
      `${pages} Seiten mit ${versions} Versionen, ${files} Dateien und ${comments} Kommentaren wurden übernommen.`,
    allFound: "Alle, die das Archiv nennt, wurden hier gefunden, daher wurde jede Berechtigung übernommen.",
    people: "Hier nicht gefundene Personen",
    peopleHint: "Ihre Arbeit steht jetzt unter Ihrem Namen, ihr eigener Name bleibt im Verlauf; ihre Berechtigungen und Reaktionen wurden weggelassen.",
    groups: "Hier nicht gefundene Gruppen",
    dropped: "Weggelassene Berechtigungen",
    listNames: { view: "Leseliste", edit: "Bearbeitungsliste", editGrant: "Darf zusätzlich bearbeiten" } as Record<string, string>,
    droppedOnSpace: (permission: string, subject: string) => `${permission} für ${subject || "eine unbekannte Person"} im Bereich`,
    droppedOnPage: (permission: string, subject: string, page: string) => `${permission} für ${subject || "eine unbekannte Person"} auf ${page}`,
    reattributed: (n: number) =>
      n === 1 ? "1 Version, Kommentar oder Datei steht jetzt unter Ihrem Namen." : `${n} Versionen, Kommentare und Dateien stehen jetzt unter Ihrem Namen.`,
    droppedReactions: (n: number) => (n === 1 ? "1 Reaktion wurde weggelassen." : `${n} Reaktionen wurden weggelassen.`),
    mentionsAsText: (n: number) => (n === 1 ? "1 Erwähnung wurde zu einfachem Text." : `${n} Erwähnungen wurden zu einfachem Text.`),
    allFoundExport: "Alle, die der Export nennt, wurden hier gefunden, daher bleibt ihre Arbeit die ihre.",
    exportPermissions:
      "Ein Export bringt keine Berechtigungen mit, daher hat der Bereich die, mit denen jeder neue Bereich beginnt. Legen Sie sie in den Berechtigungen des Bereichs fest.",
    noHistory: "Ein HTML-Export enthält keinen Verlauf, daher beginnt jede Seite mit einer Version.",
    lost: "Nicht so übernommen, wie es war",
    lostHint: "Öffnen Sie diese Seiten, um zu richten, was wichtig ist; alles andere auf ihnen wurde übernommen.",
    lostEntry: (page: string, what: string) => `${page}: ${what}`,
    lostMore: (n: number) => (n === 1 ? "Und 1 weiteres." : `Und ${n} weitere.`),
    lostKinds: {
      embed: (detail: string) => `Eingebettete Inhalte (${detail}) wurden weggelassen.`,
      macro: (detail: string) => `Den Block "${detail}" gibt es hier nicht; er wurde weggelassen, sein Inhalt behalten.`,
      externalImage: (detail: string) => `Ein Bild von einer anderen Website wurde zu einem Link darauf: ${detail}`,
      missingFile: (detail: string) => (detail ? `Die Datei ${detail} fehlt im Export.` : "Ein Bild fehlt im Export."),
      outsideLink: (detail: string) => `Ein Link auf eine Seite außerhalb des Exports behielt nur seinen Text: ${detail}`,
      plainText: (_detail: string) => "Der Inhalt ließ sich nicht so lesen, wie er war, und wurde als einfacher Text behalten.",
      label: (detail: string) => `Das Stichwort ${detail} nimmt Stator nicht an; es wurde weggelassen.`,
      unreadPage: (detail: string) => `Die Seitendatei ${detail} ließ sich nicht lesen, daher ist die Seite leer.`,
      format: (_detail: string) => "Inhalt in einem älteren Format des anderen Wikis wurde als einfacher Text behalten.",
    } as Record<string, (detail: string) => string>,
  },
};
