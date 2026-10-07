import { useMemo } from "react";
import katex from "katex";
import { MATH_MAX_EXPAND, MATH_MAX_LENGTH, MATH_MAX_SIZE } from "@/config";
import { t } from "@/i18n";

// Kept apart from the editor's nodes, so the read-only view typesets a
// formula without bringing the editor along.

export const MATH_INLINE_NODE = "mathInline";
export const MATH_BLOCK_NODE = "mathBlock";

/** A formula typeset as markup, or the reason it could not be. */
export type Typeset = { html: string; error: null } | { html: null; error: string };

/**
 * Typesets TeX with KaTeX. Untrusted commands such as \href and \htmlClass
 * stay refused, so a formula draws symbols and never markup of its author's.
 */
export function typeset(latex: string, display: boolean): Typeset {
  try {
    const html = katex.renderToString(latex, {
      displayMode: display,
      throwOnError: true,
      trust: false,
      strict: "ignore",
      maxExpand: MATH_MAX_EXPAND,
      maxSize: MATH_MAX_SIZE,
      // MathML alongside, so a screen reader reads the formula rather than its glyphs.
      output: "htmlAndMathml",
    });
    return { html, error: null };
  } catch (err) {
    return { html: null, error: err instanceof katex.ParseError ? err.rawMessage : String(err) };
  }
}

/** A formula source the API takes: something other than spaces, cut at its limit; null otherwise. */
export function mathSource(value: unknown): string | null {
  if (typeof value !== "string" || !value.trim()) return null;
  return [...value].slice(0, MATH_MAX_LENGTH).join("");
}

/** A formula as readers see it; one KaTeX cannot read shows its source and says so. */
export function MathFormula({ latex, display }: { latex: string; display: boolean }) {
  const out = useMemo(() => typeset(latex, display), [latex, display]);
  const Tag = display ? "div" : "span";
  const kind = display ? "block" : "inline";
  if (out.html === null) {
    return (
      <Tag className="doc-math" data-math={kind} data-math-error="" title={out.error}>
        <span className="sr-only">{t.editor.math.unreadable} </span>
        <code>{latex}</code>
      </Tag>
    );
  }
  // KaTeX escapes the source and, without trust, draws no link or markup of the author's.
  return <Tag className="doc-math" data-math={kind} dangerouslySetInnerHTML={{ __html: out.html }} />;
}
