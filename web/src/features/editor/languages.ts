import { common, createLowlight } from "lowlight";
import { CODE_LANGUAGES } from "@/config";

/** The highlighter, with exactly the grammars the language picker offers. */
export const lowlight = createLowlight(
  Object.fromEntries(
    CODE_LANGUAGES.map(({ id }) => {
      const grammar = common[id];
      if (!grammar) throw new Error(`CODE_LANGUAGES names ${id}, which the highlighter does not ship.`);
      return [id, grammar];
    }),
  ),
);

/** The name the picker shows for a language, or the language itself when it offers none. */
export function languageLabel(id: string): string {
  return CODE_LANGUAGES.find((language) => language.id === id)?.label ?? id;
}
