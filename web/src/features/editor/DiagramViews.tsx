import { useEffect, useState } from "react";
import type MermaidAPI from "mermaid";
import { Button } from "@/components/ui";
import { Icon } from "@/components/icons";
import { DIAGRAM_FILE_NAME, DIAGRAM_MAX_EDGES, DIAGRAM_MAX_LENGTH } from "@/config";
import { t } from "@/i18n";

// Kept apart from the editor's node, so the read-only view draws a diagram
// without bringing the editor along.

export const DIAGRAM_NODE = "diagram";

/** A source the API takes: something other than spaces, cut at its limit; null otherwise. */
export function diagramSource(value: unknown): string | null {
  if (typeof value !== "string" || !value.trim()) return null;
  return [...value].slice(0, DIAGRAM_MAX_LENGTH).join("");
}

/** A diagram drawn as SVG, or the reason it could not be. */
export type Drawn = { svg: string; error: null } | { svg: null; error: string };

type Mermaid = typeof MermaidAPI;

// Mermaid is large, so it loads with the first diagram rather than with every page.
let mermaid: Promise<Mermaid> | null = null;
function loadMermaid(): Promise<Mermaid> {
  mermaid ??= import("mermaid").then((m) => m.default);
  return mermaid;
}

const HEX = /^#(?:[0-9a-f]{3}|[0-9a-f]{6})$/i;

/**
 * The page's own colours, so a diagram follows the theme. Mermaid computes
 * shades from them and reads only hex, so a theme written otherwise gets
 * Mermaid's light or dark set instead.
 */
export function diagramTheme(style: CSSStyleDeclaration, dark: boolean): { theme: "base" | "default" | "dark"; themeVariables?: Record<string, string> } {
  const token = (name: string) => style.getPropertyValue(name).trim();
  const themeVariables = {
    background: token("--color-surface"),
    primaryColor: token("--color-surface-raised"),
    primaryTextColor: token("--color-ink"),
    primaryBorderColor: token("--color-border-strong"),
    secondaryColor: token("--color-accent-subtle"),
    tertiaryColor: token("--color-surface-sunken"),
    lineColor: token("--color-ink-muted"),
    textColor: token("--color-ink"),
  };
  if (Object.values(themeVariables).every((value) => HEX.test(value))) return { theme: "base", themeVariables: { ...themeVariables, darkMode: String(dark) } };
  return { theme: dark ? "dark" : "default" };
}

function pageIsDark(): boolean {
  const chosen = document.documentElement.getAttribute("data-theme");
  if (chosen === "dark" || chosen === "light") return chosen === "dark";
  return window.matchMedia?.("(prefers-color-scheme: dark)").matches ?? false;
}

let drawn = 0;

/**
 * Draws Mermaid text as SVG. Strict security drops click handlers and
 * scripts and text labels keep tags as words, so a diagram draws shapes and
 * words only.
 */
export async function drawDiagram(source: string): Promise<Drawn> {
  const id = `stator-diagram-${++drawn}`;
  try {
    const m = await loadMermaid();
    m.initialize({
      startOnLoad: false,
      securityLevel: "strict",
      // Labels as SVG text rather than HTML, so a tag in one is never an
      // element: strict mode alone keeps an <img> that would fetch its source.
      htmlLabels: false,
      flowchart: { htmlLabels: false },
      maxTextSize: DIAGRAM_MAX_LENGTH,
      maxEdges: DIAGRAM_MAX_EDGES,
      fontFamily: getComputedStyle(document.body).fontFamily,
      ...diagramTheme(getComputedStyle(document.documentElement), pageIsDark()),
    });
    await m.parse(source);
    const { svg } = await m.render(id, source);
    return { svg, error: null };
  } catch (err) {
    // A failed render leaves its scratch element behind in the page.
    document.getElementById(id)?.remove();
    document.getElementById(`d${id}`)?.remove();
    return { svg: null, error: err instanceof Error ? err.message : String(err) };
  }
}

/** The diagram for a source, drawn once the source has stood still for the delay given. */
export function useDiagram(source: string, delay = 0): Drawn | null {
  const [out, setOut] = useState<{ source: string; drawn: Drawn } | null>(null);
  useEffect(() => {
    let live = true;
    const timer = setTimeout(() => {
      void drawDiagram(source).then((result) => {
        if (live) setOut({ source, drawn: result });
      });
    }, delay);
    return () => {
      live = false;
      clearTimeout(timer);
    };
  }, [source, delay]);
  // While a new drawing is on its way the last one stays, so the preview does not flicker.
  return out?.drawn ?? null;
}

/** A drawn diagram, or its source while it is drawn and when it cannot be, saying why. */
export function DiagramDrawing({ source, drawn }: { source: string; drawn: Drawn | null }) {
  if (drawn?.svg) {
    // Mermaid's strict mode sanitises what it draws; see drawDiagram.
    return (
      <div className="doc-diagram-svg" data-diagram-svg="" role="img" aria-label={t.editor.diagram.drawing} dangerouslySetInnerHTML={{ __html: drawn.svg }} />
    );
  }
  return (
    <div className="doc-diagram-source" data-diagram-state={drawn ? "error" : "drawing"}>
      {drawn?.error && (
        <p className="doc-diagram-error" data-diagram-error="" title={drawn.error}>
          {t.editor.diagram.unreadable(drawn.error.split("\n")[0] ?? "")}
        </p>
      )}
      <pre>
        <code>{source}</code>
      </pre>
    </div>
  );
}

function download(svg: string) {
  const url = URL.createObjectURL(new Blob([svg], { type: "image/svg+xml" }));
  const link = document.createElement("a");
  link.href = url;
  link.download = DIAGRAM_FILE_NAME;
  link.click();
  URL.revokeObjectURL(url);
}

/** A diagram as readers see it, with its drawing to save as an SVG file. */
export function DiagramFigure({ source }: { source: string }) {
  const drawn = useDiagram(source);
  return (
    <figure className="doc-diagram" data-diagram="">
      <DiagramDrawing source={source} drawn={drawn} />
      {drawn?.svg && (
        <figcaption className="doc-diagram-tools" data-print-hide="">
          <Button type="button" variant="ghost" size="sm" onClick={() => download(drawn.svg)} data-action="download-diagram">
            <Icon.Download size={14} />
            {t.editor.diagram.download}
          </Button>
        </figcaption>
      )}
    </figure>
  );
}
