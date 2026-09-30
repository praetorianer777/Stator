import type { Locator } from "@playwright/test";

// ProseMirror puts its own selection back into the page this long after the
// editor gains focus (prosemirror-view's focus handler), in case the browser
// moved the caret on focus. A caret key the browser carried out before then,
// and before ProseMirror read the move, is undone: the next words land where
// the click put the caret.
const PROSEMIRROR_FOCUS_SYNC_MS = 20;

/** Clicks into a rich text editor, or a spot inside one, and waits until a caret key pressed next stays pressed. */
export async function focusEditor(target: Locator): Promise<void> {
  await target.click();
  // Timers of the same delay run in the order they were set, so this one ends
  // after ProseMirror's, which the click set.
  await target.evaluate((_, ms) => new Promise<void>((done) => setTimeout(done, ms)), PROSEMIRROR_FOCUS_SYNC_MS);
}

/** Puts the caret at the start or the end of the editor's document. */
export async function caretTo(box: Locator, where: "start" | "end"): Promise<void> {
  await focusEditor(box);
  await box.page().keyboard.press(where === "start" ? "ControlOrMeta+Home" : "ControlOrMeta+End");
}
