import type { collabEn } from "./collab.en";

/** The German strings of editing together, held to the English shape. */
export const collabDe: typeof collabEn = {
  editingNow: (names: string) => `Bearbeiten gerade: ${names}`,
  more: (n: number) => `+${n}`,
  connecting: "Verbindung zu den anderen wird hergestellt",
  offline: "Offline. Schreiben Sie weiter: Ihre Änderungen werden zusammengeführt, sobald die Verbindung zurück ist.",
  saved: "Gespeichert. Alle, die mitschreiben, sehen Ihre Änderungen beim Tippen.",
  signedOut:
    "Ihre Sitzung ist abgelaufen, Ihre Änderungen werden also nicht mehr geteilt. Kopieren Sie, was Sie geschrieben haben, melden Sie sich neu an und öffnen Sie den Editor noch einmal.",
  refused:
    "Sie dürfen diese Seite nicht mehr bearbeiten, Ihre Änderungen werden also nicht mehr geteilt. Bitten Sie jemanden, der den Bereich verwaltet, um Bearbeitungsrechte.",
  confirmDiscard: "Er geht für alle verloren, die ihn bearbeiten. Die Seite bleibt, wie sie zuletzt veröffentlicht wurde.",
};
