import { describe, expect, it } from "vitest";
import { readFileSync } from "node:fs";
import { join } from "node:path";
import { TOKEN_GROUPS } from "@/lib/theme-tokens";

// WCAG AA for body text. A token used only for large text or icons may be
// listed in LOWER_BAR at AA_LARGE_TEXT_OR_ICON.
const AA_TEXT = 4.5;
const AA_LARGE_TEXT_OR_ICON = 3;

const TEXT_TOKENS = TOKEN_GROUPS.find((group) => group.id === "text")!.tokens;
const SURFACE_TOKENS = ["canvas", "surface", "surface-raised", "surface-sunken", "surface-overlay"];

// Every text token is held to AA_TEXT except the ones named here, each with
// the reason it may sit lower. Add a token only with a reason that holds.
const NO_BAR = 1;
const LOWER_BAR: Record<string, { ratio: number; reason: string }> = {
  "ink-disabled": {
    ratio: NO_BAR,
    reason: "WCAG 1.4.3 exempts the text of inactive controls, and the faded ink is what tells a person the control is off",
  },
};

type Palette = Record<string, string>;

function channel(hex: string, offset: number): number {
  const value = parseInt(hex.slice(offset, offset + 2), 16) / 255;
  return value <= 0.04045 ? value / 12.92 : ((value + 0.055) / 1.055) ** 2.4;
}

function luminance(hex: string): number {
  return 0.2126 * channel(hex, 1) + 0.7152 * channel(hex, 3) + 0.0722 * channel(hex, 5);
}

/** The WCAG contrast ratio of two #rrggbb colours. */
function contrast(a: string, b: string): number {
  const [light, dark] = [luminance(a), luminance(b)].sort((x, y) => y - x);
  return (light! + 0.05) / (dark! + 0.05);
}

function declarations(block: string): Palette {
  return Object.fromEntries([...block.matchAll(/--color-([a-z0-9-]+):\s*([^;]+);/g)].map((m) => [m[1]!, m[2]!.trim().toLowerCase()]));
}

function goMap(body: string): Palette {
  return Object.fromEntries([...body.matchAll(/"([a-z0-9-]+)":\s*"([^"]+)"/g)].map((m) => [m[1]!, m[2]!.toLowerCase()]));
}

const css = readFileSync(join(__dirname, "index.css"), "utf8");
const clayLight = declarations(css.match(/@theme \{([\s\S]*?)\n\}/)![1]!);
const clayDarkChoice = declarations(css.match(/\n {2}:root\[data-theme="dark"\] \{([\s\S]*?)\n {2}\}/)![1]!);
const clayDarkSystem = declarations(css.match(/@media \(prefers-color-scheme: dark\) \{([\s\S]*?)\n {4}\}/)![1]!);

const CODE_BACKGROUND = css.match(/\.doc-content pre \{[^}]*background: var\(--color-([a-z0-9-]+)\)/)![1]!;
// Read from the rules that paint highlighted code, so a token a new rule
// hands to code is judged without anyone remembering to list it here.
const HIGHLIGHT_TOKENS = [
  ...new Set(
    [...css.matchAll(/[^{}]*\.hljs-[^{}]*\{([^{}]*)\}/g)].flatMap((m) => [...m[1]!.matchAll(/\bcolor: var\(--color-([a-z0-9-]+)\)/g)].map((c) => c[1]!)),
  ),
];

interface Example {
  key: string;
  light: Palette;
  dark: Palette;
  css: string;
}

// The shipped themes are Go, the one place they are written down; reading
// the source keeps this test from trusting a copy that could drift.
function readExamples(): Example[] {
  const source = readFileSync(join(__dirname, "..", "..", "..", "backend", "internal", "theme", "examples.go"), "utf8");
  const bodies = new Map<string, string>();
  const funcs = [...source.matchAll(/\nfunc (\w+)\(/g)];
  funcs.forEach((m, i) => bodies.set(m[1]!, source.slice(m.index, funcs[i + 1]?.index)));

  const build = (name: string): Example => {
    const body = bodies.get(name);
    if (!body) throw new Error(`examples.go has no func ${name}() Example`);
    const base = body.match(/base := (\w+)\(\)/)?.[1];
    const inherited = base ? build(base) : { light: {}, dark: {} };
    const light: Palette = { ...inherited.light, ...goMap(body.match(/\blight := map\[string\]string\{([\s\S]*?)\n\t\}/)?.[1] ?? "") };
    const dark: Palette = { ...inherited.dark, ...goMap(body.match(/\bdark := map\[string\]string\{([\s\S]*?)\n\t\}/)?.[1] ?? "") };
    for (const m of body.matchAll(/range map\[string\]string\{([\s\S]*?)\}\s*\{\s*(light|dark)\[k\] = v/g)) {
      Object.assign(m[2] === "light" ? light : dark, goMap(m[1]!));
    }
    return { key: body.match(/Key:\s*"([^"]+)"/)![1]!, light, dark, css: body.match(/css := `([^`]*)`/)?.[1] ?? "" };
  };

  const listed = source.match(/examples := \[\]Example\{([^}]*)\}/)![1]!;
  return [...listed.matchAll(/(\w+)\(\)/g)].map((m) => build(m[1]!));
}

interface Case {
  name: string;
  palette: Palette;
  surfaces: string[];
}

// A theme's palette lands on top of the stylesheet, so what it leaves out is Clay's.
function cases(): Case[] {
  const out: Case[] = [
    { name: "Clay light", palette: clayLight, surfaces: SURFACE_TOKENS },
    { name: "Clay dark", palette: { ...clayLight, ...clayDarkChoice }, surfaces: SURFACE_TOKENS },
  ];
  for (const example of readExamples()) {
    for (const [mode, own, clay] of [
      ["light", example.light, clayLight],
      ["dark", example.dark, { ...clayLight, ...clayDarkChoice }],
    ] as const) {
      const palette = { ...clay, ...own };
      out.push({ name: `${example.key} ${mode}`, palette, surfaces: SURFACE_TOKENS });
      // A rule in the theme's CSS that repaints a part of the page, such as
      // Constellation's dark column, is judged on the surfaces it repaints.
      for (const m of example.css.replace(/\/\*[\s\S]*?\*\//g, "").matchAll(/([^{}]+)\{([^{}]*)\}/g)) {
        const scoped = declarations(m[2]!);
        const surfaces = SURFACE_TOKENS.filter((token) => token in scoped);
        if (surfaces.length === 0) continue;
        out.push({ name: `${example.key} ${mode}, ${m[1]!.trim()}`, palette: { ...palette, ...scoped }, surfaces });
      }
    }
  }
  return out;
}

describe("text contrast", () => {
  it("reads both dark blocks and every shipped theme", () => {
    expect(clayDarkSystem).toEqual(clayDarkChoice);
    const names = cases().map((c) => c.name);
    expect(names).toEqual(expect.arrayContaining(["deep-tech light", "deep-tech dark", "constellation light", "constellation dark"]));
    expect(names.filter((name) => name.startsWith("constellation") && name.includes("[data-rail]"))).toHaveLength(2);
    for (const { palette } of cases()) {
      for (const token of [...TEXT_TOKENS, ...SURFACE_TOKENS]) expect(palette[token], token).toMatch(/^#[0-9a-f]{6}$/);
    }
  });

  it("lowers the bar only for text tokens, each with a reason", () => {
    for (const [token, { ratio, reason }] of Object.entries(LOWER_BAR)) {
      expect(TEXT_TOKENS).toContain(token);
      expect([AA_LARGE_TEXT_OR_ICON, NO_BAR]).toContain(ratio);
      expect(reason).not.toBe("");
    }
  });

  describe.each(cases())("$name", ({ palette, surfaces }) => {
    const pairs = TEXT_TOKENS.flatMap((text) => surfaces.map((surface) => ({ text, surface })));
    it.each(pairs)("$text on $surface", ({ text, surface }) => {
      const needed = LOWER_BAR[text]?.ratio ?? AA_TEXT;
      const ratio = contrast(palette[text]!, palette[surface]!);
      expect(ratio, `${text} ${palette[text]} on ${surface} ${palette[surface]} is ${ratio.toFixed(2)}:1`).toBeGreaterThanOrEqual(needed);
    });
  });
});

// Coloured words: links and the current item in the accent, errors and
// destructive menu items in danger, each on any surface and on its own tint.
const COLOURED_TEXT: Array<{ text: string; tint: string }> = [
  { text: "accent", tint: "accent-subtle" },
  { text: "danger", tint: "danger-subtle" },
];

// The fills a label is written on, with the token that writes it, hover included.
const FILLS: Array<{ text: string; fill: string }> = [
  { text: "on-primary", fill: "primary" },
  { text: "on-primary", fill: "primary-hover" },
  { text: "on-accent", fill: "accent" },
  { text: "on-accent", fill: "accent-hover" },
  { text: "on-danger", fill: "danger" },
  { text: "on-danger", fill: "danger-hover" },
];

describe("coloured text and labels on fills", () => {
  it("reads every token it judges as a colour", () => {
    for (const { palette } of cases()) {
      for (const token of [...COLOURED_TEXT.flatMap((c) => [c.text, c.tint]), ...FILLS.flatMap((f) => [f.text, f.fill])]) {
        expect(palette[token], token).toMatch(/^#[0-9a-f]{6}$/);
      }
    }
  });

  describe.each(cases())("$name", ({ palette, surfaces }) => {
    const pairs = [
      ...COLOURED_TEXT.flatMap(({ text, tint }) => [...surfaces, tint].map((background) => ({ text, background }))),
      ...FILLS.map(({ text, fill }) => ({ text, background: fill })),
    ];
    it.each(pairs)("$text on $background", ({ text, background }) => {
      const ratio = contrast(palette[text]!, palette[background]!);
      expect(ratio, `${text} ${palette[text]} on ${background} ${palette[background]} is ${ratio.toFixed(2)}:1`).toBeGreaterThanOrEqual(AA_TEXT);
    });
  });
});

// Read from the rules that paint a status label, so a colour added there is
// judged without anyone remembering to list it here.
const STATUS_TEXT = css.match(/\.doc-status \{[^}]*\bcolor: var\(--color-([a-z0-9-]+)\)/)![1]!;
const STATUS_FILLS = Object.fromEntries(
  [...css.matchAll(/\[data-status-label="([a-z]+)"\] \{\s*--status-fill: var\(--color-([a-z0-9-]+)\);/g)].map((m) => [m[1]!, m[2]!]),
);

describe("status label contrast", () => {
  it("reads every colour a status can have, and the ink it is written in", () => {
    expect(STATUS_TEXT).toBe("ink");
    const allowlist = JSON.parse(readFileSync(join(__dirname, "..", "..", "..", "api", "document-allowlist.json"), "utf8")) as {
      nodes: Record<string, { attrs?: Record<string, { enum?: string[] }> }>;
    };
    expect(Object.keys(STATUS_FILLS).sort()).toEqual([...(allowlist.nodes.status?.attrs?.color?.enum ?? [])].sort());
    for (const { palette } of cases()) {
      for (const token of [STATUS_TEXT, ...Object.values(STATUS_FILLS)]) expect(palette[token], token).toMatch(/^#[0-9a-f]{6}$/);
    }
  });

  // A status sits in a page, so a theme rule that repaints only another part
  // of the screen, such as a dark rail, never paints behind one.
  describe.each(cases().filter((c) => c.surfaces === SURFACE_TOKENS))("$name", ({ palette }) => {
    it.each(Object.entries(STATUS_FILLS))(`${STATUS_TEXT} on the %s status, %s`, (_colour, fill) => {
      const ratio = contrast(palette[STATUS_TEXT]!, palette[fill]!);
      expect(ratio, `${STATUS_TEXT} ${palette[STATUS_TEXT]} on ${fill} ${palette[fill]} is ${ratio.toFixed(2)}:1`).toBeGreaterThanOrEqual(AA_TEXT);
    });
  });
});

describe("code highlighting contrast", () => {
  it("reads the code block background and every colour the highlighting uses", () => {
    expect(CODE_BACKGROUND).toBe("surface-sunken");
    expect(HIGHLIGHT_TOKENS).toEqual(expect.arrayContaining(["chart-1", "chart-2", "chart-3", "chart-4", "chart-5", "chart-6", "success", "danger"]));
    for (const { palette } of cases()) {
      for (const token of HIGHLIGHT_TOKENS) expect(palette[token], token).toMatch(/^#[0-9a-f]{6}$/);
    }
  });

  describe.each(cases().filter((c) => c.surfaces.includes(CODE_BACKGROUND)))("$name", ({ palette }) => {
    it.each(HIGHLIGHT_TOKENS)(`%s on ${CODE_BACKGROUND}`, (token) => {
      const ratio = contrast(palette[token]!, palette[CODE_BACKGROUND]!);
      expect(ratio, `${token} ${palette[token]} on ${CODE_BACKGROUND} ${palette[CODE_BACKGROUND]} is ${ratio.toFixed(2)}:1`).toBeGreaterThanOrEqual(AA_TEXT);
    });
  });
});

// Read from the rules that paint the verification badge, so a state added
// there is judged without anyone remembering to list it here.
const BADGE_TEXT = css.match(/\.verification-badge \{[^}]*\bcolor: var\(--color-([a-z0-9-]+)\)/)![1]!;
const BADGE_STATES = Object.fromEntries(
  [
    ...css.matchAll(
      /\[data-verification-badge="([a-z]+)"\] \{\s*--verification-fill: var\(--color-([a-z0-9-]+)\);\s*--verification-glyph: var\(--color-([a-z0-9-]+)\);/g,
    ),
  ].map((m) => [m[1]!, { fill: m[2]!, glyph: m[3]! }]),
);

describe("verification badge contrast", () => {
  it("reads both states, the ink the words are in and each glyph's colour", () => {
    expect(BADGE_TEXT).toBe("ink");
    expect(Object.keys(BADGE_STATES).sort()).toEqual(["expired", "verified"]);
    for (const { palette } of cases()) {
      for (const { fill, glyph } of Object.values(BADGE_STATES)) {
        expect(palette[fill], fill).toMatch(/^#[0-9a-f]{6}$/);
        expect(palette[glyph], glyph).toMatch(/^#[0-9a-f]{6}$/);
      }
    }
  });

  // A badge sits in a page's header and in lists, never on a repainted rail.
  describe.each(cases().filter((c) => c.surfaces === SURFACE_TOKENS))("$name", ({ palette }) => {
    it.each(Object.entries(BADGE_STATES))("the %s badge", (_state, { fill, glyph }) => {
      const words = contrast(palette[BADGE_TEXT]!, palette[fill]!);
      expect(words, `${BADGE_TEXT} ${palette[BADGE_TEXT]} on ${fill} ${palette[fill]} is ${words.toFixed(2)}:1`).toBeGreaterThanOrEqual(AA_TEXT);
      const mark = contrast(palette[glyph]!, palette[fill]!);
      expect(mark, `${glyph} ${palette[glyph]} on ${fill} ${palette[fill]} is ${mark.toFixed(2)}:1`).toBeGreaterThanOrEqual(AA_LARGE_TEXT_OR_ICON);
    });
  });
});

// Read from the rules that paint the archive mark and banner, so a change of
// tint there is judged without anyone remembering to list it here.
const ARCHIVE_FILL = css.match(/\.archive-mark \{[^}]*\bbackground: var\(--color-([a-z0-9-]+)\)/)![1]!;
const ARCHIVE_TEXT = css.match(/\.archive-mark \{[^}]*\bcolor: var\(--color-([a-z0-9-]+)\)/)![1]!;
const ARCHIVE_GLYPH = css.match(/\.archive-mark svg \{[^}]*\bcolor: var\(--color-([a-z0-9-]+)\)/)![1]!;

describe("archive mark contrast", () => {
  it("reads the tint, the ink of the words and the glyph's colour from every palette", () => {
    expect(ARCHIVE_TEXT).toBe("ink");
    for (const { palette } of cases()) {
      for (const token of [ARCHIVE_FILL, ARCHIVE_TEXT, ARCHIVE_GLYPH]) {
        expect(palette[token], token).toMatch(/^#[0-9a-f]{6}$/);
      }
    }
  });

  // The mark sits in page headers, lists and a page's banner, never on a repainted rail.
  describe.each(cases().filter((c) => c.surfaces === SURFACE_TOKENS))("$name", ({ palette }) => {
    it("keeps its words and its glyph readable on the tint", () => {
      const words = contrast(palette[ARCHIVE_TEXT]!, palette[ARCHIVE_FILL]!);
      expect(words, `${ARCHIVE_TEXT} on ${ARCHIVE_FILL} ${palette[ARCHIVE_FILL]} is ${words.toFixed(2)}:1`).toBeGreaterThanOrEqual(AA_TEXT);
      const mark = contrast(palette[ARCHIVE_GLYPH]!, palette[ARCHIVE_FILL]!);
      expect(mark, `${ARCHIVE_GLYPH} on ${ARCHIVE_FILL} ${palette[ARCHIVE_FILL]} is ${mark.toFixed(2)}:1`).toBeGreaterThanOrEqual(AA_LARGE_TEXT_OR_ICON);
    });
  });
});

// Read from the rules that paint a task's due day, so a state added there is
// judged without anyone remembering to list it here.
const DUE_TEXT = css.match(/\.task-due \{[^}]*\bcolor: var\(--color-([a-z0-9-]+)\)/)![1]!;
const DUE_STATES = Object.fromEntries(
  [...css.matchAll(/\[data-task-due="([a-z]+)"\] \{\s*--task-due-fill: var\(--color-([a-z0-9-]+)\);\s*--task-due-glyph: var\(--color-([a-z0-9-]+)\);/g)].map(
    (m) => [m[1]!, { fill: m[2]!, glyph: m[3]! }],
  ),
);

describe("task due day contrast", () => {
  it("reads every state, the ink of the words and each glyph's colour", () => {
    expect(DUE_TEXT).toBe("ink");
    expect(Object.keys(DUE_STATES).sort()).toEqual(["overdue", "today", "upcoming"]);
    for (const { palette } of cases()) {
      for (const { fill, glyph } of Object.values(DUE_STATES)) {
        expect(palette[fill], fill).toMatch(/^#[0-9a-f]{6}$/);
        expect(palette[glyph], glyph).toMatch(/^#[0-9a-f]{6}$/);
      }
    }
  });

  // A due day sits in a page and in the list of tasks, never on a repainted rail.
  describe.each(cases().filter((c) => c.surfaces === SURFACE_TOKENS))("$name", ({ palette }) => {
    it.each(Object.entries(DUE_STATES))("a task %s", (_state, { fill, glyph }) => {
      const words = contrast(palette[DUE_TEXT]!, palette[fill]!);
      expect(words, `${DUE_TEXT} ${palette[DUE_TEXT]} on ${fill} ${palette[fill]} is ${words.toFixed(2)}:1`).toBeGreaterThanOrEqual(AA_TEXT);
      const mark = contrast(palette[glyph]!, palette[fill]!);
      expect(mark, `${glyph} ${palette[glyph]} on ${fill} ${palette[fill]} is ${mark.toFixed(2)}:1`).toBeGreaterThanOrEqual(AA_LARGE_TEXT_OR_ICON);
    });
  });
});
