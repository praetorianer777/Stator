import { useLinkPreview, type LinkPreview } from "@/api/linkPreview";
import { Icon } from "@/components/icons";
import { t } from "@/i18n";

// Kept apart from the editor's node, so the read-only view draws a card
// without bringing the editor along.

export const LINK_CARD_NODE = "linkCard";

export type LinkCardView = "card" | "embed";

/** An address a card may lead to: a web page's, http or https; null otherwise. */
export function webAddress(value: unknown): string | null {
  if (typeof value !== "string") return null;
  const trimmed = value.trim();
  if (!/^https?:\/\/\S+$/i.test(trimmed)) return null;
  try {
    const url = new URL(trimmed);
    return url.host && !url.username && !url.password ? trimmed : null;
  } catch {
    return null;
  }
}

export function linkCardView(value: unknown): LinkCardView {
  return value === "embed" ? "embed" : "card";
}

/** An address as a reader recognises it: its host and path, without the scheme. */
export function shortAddress(url: string): string {
  try {
    const u = new URL(url);
    const path = u.pathname === "/" ? "" : u.pathname;
    return `${u.host.replace(/^www\./, "")}${path}`;
  } catch {
    return url;
  }
}

const EMBED_ALLOW = "fullscreen; picture-in-picture; encrypted-media; clipboard-write";
// The player runs its own scripts on its own origin; it may open its site in
// a new tab, and nothing else of the page's.
const EMBED_SANDBOX = "allow-scripts allow-same-origin allow-popups allow-popups-to-escape-sandbox allow-presentation";

function Embed({ preview }: { preview: LinkPreview }) {
  const embed = preview.embed!;
  return (
    <div className="doc-link-embed" data-link-embed={embed.kind}>
      <iframe
        src={embed.src}
        title={t.editor.linkCard.embedTitle(embed.provider, preview.title || shortAddress(preview.url))}
        allow={EMBED_ALLOW}
        sandbox={EMBED_SANDBOX}
        referrerPolicy="strict-origin-when-cross-origin"
        loading="lazy"
        allowFullScreen
      />
    </div>
  );
}

function CardBody({ url, preview }: { url: string; preview: LinkPreview | undefined }) {
  const title = preview?.title || shortAddress(url);
  return (
    <>
      <span className="doc-link-card-site">
        <Icon.Link size={12} />
        {preview?.siteName || shortAddress(url).split("/")[0]}
      </span>
      <span className="doc-link-card-title" data-link-card-title="">
        {title}
      </span>
      {preview?.description && <span className="doc-link-card-description">{preview.description}</span>}
      <span className="doc-link-card-url">{shortAddress(url)}</span>
    </>
  );
}

/**
 * A link as a card with what its page says about itself, or as its site's
 * player when it asks for one and the site is allowlisted. Links is false in
 * the editor, where a click selects the card.
 */
export function LinkCard({ url, view, links = true }: { url: string; view: LinkCardView; links?: boolean }) {
  const { data: preview, isPending } = useLinkPreview(url);
  const embedded = view === "embed" && preview?.embed;
  const common = { "data-link-card": view, "data-state": isPending ? "loading" : preview?.fetched ? "read" : "plain" };
  return (
    <div className="doc-link-card" {...common}>
      {embedded && <Embed preview={preview} />}
      {links ? (
        <a href={url} target="_blank" rel="noopener noreferrer nofollow" className="doc-link-card-body">
          <CardBody url={url} preview={preview} />
        </a>
      ) : (
        <div className="doc-link-card-body">
          <CardBody url={url} preview={preview} />
        </div>
      )}
    </div>
  );
}
