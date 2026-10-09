import { describe, expect, it } from "vitest";
import { CARET_MENU_ROOM, caretMenuPlace } from "./caretMenu";

const room = (place: object) => (place as Record<string, string>)[CARET_MENU_ROOM];

const window = { width: 1200, height: 800 };
const caret = (top: number) => ({ left: 100, top, bottom: top + 20 });

describe("a menu drawn at the caret", () => {
  it("opens below it when there is room, at its full height", () => {
    expect(caretMenuPlace(caret(100), 288, window)).toEqual({ left: 100, top: 124, [CARET_MENU_ROOM]: `${800 - 120 - 4 - 8}px` });
  });

  it("opens above it near the bottom, hanging from the caret so it cannot pass the edge", () => {
    const place = caretMenuPlace(caret(740), 288, window);
    expect(place).toEqual({ left: 100, bottom: 800 - 740 + 4, [CARET_MENU_ROOM]: `${740 - 4 - 8}px` });
    expect(place.top).toBeUndefined();
  });

  it("shortens to the room it has on the side it takes", () => {
    expect(room(caretMenuPlace(caret(600), 288, window))).toBe(`${800 - 620 - 4 - 8}px`);
    expect(caretMenuPlace(caret(200), 288, { width: 1200, height: 260 })).toMatchObject({ bottom: 260 - 200 + 4, [CARET_MENU_ROOM]: `${200 - 4 - 8}px` });
  });

  it("stays below when the room is poor on both sides but better below", () => {
    expect(caretMenuPlace(caret(40), 288, { width: 1200, height: 160 })).toMatchObject({ top: 64 });
  });

  it("keeps clear of the window's sides", () => {
    expect(caretMenuPlace({ left: 1150, top: 100, bottom: 120 }, 288, window).left).toBe(1200 - 288 - 8);
    expect(caretMenuPlace({ left: -20, top: 100, bottom: 120 }, 288, window).left).toBe(8);
  });
});
