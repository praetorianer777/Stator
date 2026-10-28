import { useContext, useEffect, useState } from "react";
import type MermaidAPI from "mermaid";
import { Button } from "@/components/ui";
import { Icon } from "@/components/icons";
import { API_BASE, DIAGRAM_FILE_NAME, DIAGRAM_KROKI_LANGUAGES, DIAGRAM_MAX_EDGES, DIAGRAM_MAX_LENGTH } from "@/config";
import { t } from "@/i18n";
import { ApiError, api } from "@/api/client";
import { sha256Hex } from "@/lib/sha256";
import { DocPageContext } from "./BlockViews";
import { usePublicLink, usePublicPageId, usePublicReading } from "./publicReading";

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

/** Whether the page shows in its dark theme, chosen or followed from the system. */
export function pageIsDark(): boolean {
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

/** The language Kroki draws a diagram in, or null for Mermaid, which is drawn here. */
export function krokiLanguage(value: unknown): string | null {
  return typeof value === "string" && DIAGRAM_KROKI_LANGUAGES.includes(value) ? value : null;
}

/** Where the api serves a diagram of the page being read, by its language and source; null outside a page. */
function useDiagramUrl(language: string, source: string): string | null {
  const reading = usePublicReading();
  const token = usePublicLink();
  const publicPage = usePublicPageId();
  const page = useContext(DocPageContext);
  const hash = sha256Hex(`${language}\n${source}`);
  if (reading && token) return `${API_BASE}/public/links/${encodeURIComponent(token)}/diagrams/${hash}`;
  if (reading) return publicPage ? `${API_BASE}/public/pages/${encodeURIComponent(publicPage)}/diagrams/${hash}` : null;
  return page ? `${API_BASE}/pages/${encodeURIComponent(page.id)}/diagrams/${hash}` : null;
}

/**
 * A diagram Kroki draws, as readers and prints see it: the api's drawing as
 * an image, never put into the page, or its source while that cannot be had.
 */
export function KrokiFigure({ language, source }: { language: string; source: string }) {
  const url = useDiagramUrl(language, source);
  return (
    <figure className="doc-diagram" data-diagram="" data-diagram-language={language}>
      {url ? <KrokiDrawing key={url} url={url} source={source} /> : <KrokiSource source={source} />}
    </figure>
  );
}

function KrokiDrawing({ url, source }: { url: string; source: string }) {
  const [state, setState] = useState<"drawing" | "drawn" | "failed">("drawing");
  if (state === "failed") return <KrokiSource source={source} />;
  return (
    <div className="doc-diagram-svg doc-diagram-image" data-diagram-state={state === "drawing" ? "drawing" : undefined}>
      <img src={url} alt={t.editor.diagram.drawing} data-diagram-image="" onLoad={() => setState("drawn")} onError={() => setState("failed")} />
    </div>
  );
}

function KrokiSource({ source }: { source: string }) {
  return (
    <div className="doc-diagram-source" data-diagram-state="error">
      <p className="doc-diagram-error" data-diagram-error="">
        {t.editor.diagram.notNow}
      </p>
      <pre>
        <code>{source}</code>
      </pre>
    </div>
  );
}

/** An author's preview of a diagram Kroki draws: the drawing as an image's address, or the sentence saying why there is none. */
export type KrokiPreview = { src: string; error: null } | { src: null; error: string };

function previewError(err: unknown): string {
  if (!(err instanceof ApiError)) return t.editor.diagram.notNow;
  if (err.code === "diagram_unreadable") return t.editor.diagram.unreadable(err.message);
  return err.fields.source ?? err.fields.language ?? err.message;
}

/** Asks the api to draw a diagram for its author, as an image never put into the page. */
export async function previewKroki(language: string, source: string, signal?: AbortSignal): Promise<KrokiPreview> {
  try {
    const { data } = await api.POST("/diagrams", { body: { language, source }, parseAs: "text", signal });
    return { src: `data:image/svg+xml;charset=utf-8,${encodeURIComponent(String(data ?? ""))}`, error: null };
  } catch (err) {
    return { src: null, error: previewError(err) };
  }
}

/** The preview for a source in a language Kroki draws, asked for once the source has stood still for the delay given. */
export function useKrokiPreview(language: string, source: string, delay: number): KrokiPreview | null {
  const [out, setOut] = useState<KrokiPreview | null>(null);
  useEffect(() => {
    const abort = new AbortController();
    const timer = setTimeout(() => {
      void previewKroki(language, source, abort.signal).then((result) => {
        if (!abort.signal.aborted) setOut(result);
      });
    }, delay);
    return () => {
      abort.abort();
      clearTimeout(timer);
    };
  }, [language, source, delay]);
  // While a new drawing is on its way the last one stays, so the preview does not flicker.
  return out;
}

/** An author's preview of a diagram Kroki draws, or its source while it is drawn and when it cannot be, saying why. */
export function KrokiPreviewDrawing({ source, preview }: { source: string; preview: KrokiPreview | null }) {
  if (preview?.src) {
    return (
      <div className="doc-diagram-svg doc-diagram-image">
        <img src={preview.src} alt={t.editor.diagram.drawing} data-diagram-image="" />
      </div>
    );
  }
  return (
    <div className="doc-diagram-source" data-diagram-state={preview ? "error" : "drawing"}>
      {preview?.error && (
        <p className="doc-diagram-error" data-diagram-error="">
          {preview.error}
        </p>
      )}
      <pre>
        <code>{source}</code>
      </pre>
    </div>
  );
}
