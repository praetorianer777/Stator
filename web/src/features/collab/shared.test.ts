import { describe, expect, it } from "vitest";
import * as Y from "yjs";
import { COLLAB_COLORS, COLLAB_INK } from "@/config";
import { collaboratorsOf, colorFor, setSharedTitle, titleOf } from "./shared";

/** WCAG's contrast ratio of two #rrggbb colours. */
function contrast(a: string, b: string): number {
  const luminance = (hex: string) => {
    const [r, g, bl] = [1, 3, 5].map((i) => {
      const c = Number.parseInt(hex.slice(i, i + 2), 16) / 255;
      return c <= 0.03928 ? c / 12.92 : ((c + 0.055) / 1.055) ** 2.4;
    }) as [number, number, number];
    return 0.2126 * r + 0.7152 * g + 0.0722 * bl;
  };
  const [hi, lo] = [luminance(a), luminance(b)].sort((x, y) => y - x) as [number, number];
  return (hi + 0.05) / (lo + 0.05);
}

const MIN_CONTRAST = 4.5;

describe("the shared title", () => {
  it("changes only what differs, so changes made at once elsewhere survive", () => {
    const a = new Y.Doc();
    const b = new Y.Doc();
    titleOf(a).insert(0, "Plans for next year");
    Y.applyUpdate(b, Y.encodeStateAsUpdate(a));
    setSharedTitle(titleOf(a), "Plans for 2027");
    setSharedTitle(titleOf(b), "Our plans for next year");
    Y.applyUpdate(a, Y.encodeStateAsUpdate(b));
    Y.applyUpdate(b, Y.encodeStateAsUpdate(a));
    expect(titleOf(a).toString()).toBe("Our plans for 2027");
    expect(titleOf(b).toString()).toBe(titleOf(a).toString());
  });

  it("changes nothing when nothing differs", () => {
    const doc = new Y.Doc();
    titleOf(doc).insert(0, "Same");
    const before = Y.encodeStateVector(doc);
    setSharedTitle(titleOf(doc), "Same");
    expect(Y.encodeStateVector(doc)).toEqual(before);
  });
});

describe("people in a shared draft", () => {
  it("each keep one colour, readable under their initials", () => {
    expect(colorFor("0195f000-0000-7000-8000-0000000000a1")).toBe(colorFor("0195f000-0000-7000-8000-0000000000a1"));
    expect(COLLAB_COLORS).toContain(colorFor("anybody"));
    for (const color of COLLAB_COLORS) {
      expect(color).toMatch(/^#[0-9a-f]{6}$/);
      expect(contrast(color, COLLAB_INK)).toBeGreaterThanOrEqual(MIN_CONTRAST);
    }
  });

  it("are named once each, without the person looking", () => {
    const states = new Map<number, Record<string, unknown>>([
      [1, { user: { id: "me", name: "Me" } }],
      [2, { user: { id: "bob", name: "Bob", color: "#1d4ed8" } }],
      [3, { user: { id: "bob", name: "Bob", color: "#1d4ed8" } }],
      [4, { user: { id: "ann", name: "Ann", color: "#b45309" } }],
      [5, { user: { id: "me", name: "Me in another tab" } }],
      [6, {}],
    ]);
    expect(collaboratorsOf(states, 1, "me")).toEqual([
      { id: "ann", name: "Ann", color: "#b45309" },
      { id: "bob", name: "Bob", color: "#1d4ed8" },
    ]);
  });
});
