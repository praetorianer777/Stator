import type { remoteLinksEn } from "./remoteLinks.en";

/** The German strings of links from other tools, held to the English shape. */
export const remoteLinksDe: typeof remoteLinksEn = {
  title: "Aus anderen Werkzeugen verknüpft",
  intro: "Jedes Werkzeug setzt seine Links selbst, mit dem Token von jemandem, der die Seite bearbeiten darf.",
  group: (source: string, count: number) => `${source} (${count})`,
  putBy: (name: string) => (name ? `Gesetzt von ${name}` : "Gesetzt von jemandem, der nicht mehr hier ist"),
  remove: (title: string) => `Link ${title} entfernen`,
  removeShort: "Entfernen",
  removed: (title: string) => `Link ${title} entfernt. Das Werkzeug, das ihn gesetzt hat, erfährt es und setzt ihn nicht wieder.`,
};
