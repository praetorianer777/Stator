/**
 * Imported by the editor's tests only. The editor measures the caret to
 * scroll it into view and to place a list under it; jsdom lays nothing out,
 * so every measurement is an empty box and no point on the page holds an
 * element. Only these tests get that: axe reads elementFromPoint too, and
 * the shell's tests are written against jsdom without it.
 */
const noRects = (): DOMRectList => Object.assign([], { item: () => null }) as unknown as DOMRectList;
Range.prototype.getBoundingClientRect ??= () => new DOMRect();
Range.prototype.getClientRects ??= noRects;
document.elementFromPoint ??= () => null;
