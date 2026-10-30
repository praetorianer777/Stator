import type { reactionsEn } from "./reactions.en";

/** The German strings of reactions, held to the English shape. */
export const reactionsDe: typeof reactionsEn = {
  page: "Reaktionen auf diese Seite",
  comment: (name: string) => `Reaktionen auf den Kommentar von ${name}`,
  add: "Reaktion hinzufügen",
  picker: "Reaktion wählen",
  more: "Weitere Emoji",
  searchTitle: "Mit einem beliebigen Emoji reagieren",
  searchLabel: "Emoji nach englischem Namen suchen",
  searchPlaceholder: "Englischer Name, etwa party oder thumbs",
  searchResults: "Gefundene Emoji",
  searchEmpty: "Kein Emoji mit diesem Namen. Versuchen Sie ein anderes englisches Wort.",
  searchLoading: "Die Emoji werden geladen",
  searchFailed: "Die Emoji konnten nicht geladen werden. Schließen Sie dies und versuchen Sie es gleich noch einmal.",
  toggle: (emoji: string, n: number) => `${emoji} ${n === 1 ? "1 Reaktion" : `${n} Reaktionen`}`,
  you: "Sie",
  formerMember: "ein ehemaliges Mitglied",
  /** Who put an emoji on, the caller first, the rest counted once the names run out. */
  who: (names: string[], more: number, emoji: string) => {
    const all = more > 0 ? [...names, more === 1 ? "1 weitere Person" : `${more} weitere Personen`] : names;
    const list = all.length <= 1 ? (all[0] ?? "") : `${all.slice(0, -1).join(", ")} und ${all[all.length - 1]}`;
    return `${list} mit ${emoji} reagiert`;
  },
  names: {
    "👍": "Daumen hoch",
    "❤️": "Herz",
    "🎉": "Feiern",
    "😄": "Lächeln",
    "😮": "Überrascht",
    "👀": "Hinsehen",
    "🚀": "Rakete",
    "✅": "Erledigt",
  } as Record<string, string>,
};
