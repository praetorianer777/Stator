/** The English strings of reactions. */
export const reactionsEn = {
  page: "Reactions to this page",
  comment: (name: string) => `Reactions to ${name}'s comment`,
  add: "Add a reaction",
  picker: "Pick a reaction",
  more: "More emoji",
  searchTitle: "React with any emoji",
  searchLabel: "Find an emoji by name",
  searchPlaceholder: "Type a name, such as party or thumbs",
  searchResults: "Emoji found",
  searchEmpty: "No emoji by that name. Try another word.",
  searchLoading: "Loading the emoji",
  searchFailed: "The emoji could not be loaded. Close this and try again in a moment.",
  toggle: (emoji: string, n: number) => `${emoji} ${n === 1 ? "1 reaction" : `${n} reactions`}`,
  you: "You",
  formerMember: "a former member",
  /** Who put an emoji on, the caller first, the rest counted once the names run out. */
  who: (names: string[], more: number, emoji: string) => {
    const all = more > 0 ? [...names, more === 1 ? "1 other" : `${more} others`] : names;
    const list = all.length <= 1 ? (all[0] ?? "") : `${all.slice(0, -1).join(", ")} and ${all[all.length - 1]}`;
    return `${list} reacted with ${emoji}`;
  },
  names: {
    "👍": "Thumbs up",
    "❤️": "Heart",
    "🎉": "Celebrate",
    "😄": "Smile",
    "😮": "Surprised",
    "👀": "Looking",
    "🚀": "Rocket",
    "✅": "Done",
  } as Record<string, string>,
};
