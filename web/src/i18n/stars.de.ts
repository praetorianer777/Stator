import type { starEn } from "./stars.en";

/** The German strings of starred pages, held to the English shape. */
export const starDe: typeof starEn = {
  starPage: (title: string) => `${title} mit Stern markieren`,
  starSpace: (name: string) => `Bereich ${name} mit Stern markieren`,
  columnStar: "Stern",
  failed: "Der Stern konnte nicht geändert werden. Versuchen Sie es gleich noch einmal.",
};
