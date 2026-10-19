// biome-ignore-all lint/suspicious/noArrayIndexKey: a node has no identity but its place, and a read-only view never reorders them
import type { ReactNode } from "react";
import { EXCITER_DOCUMENT_MAX_DEPTH } from "@/config";
import { t } from "@/i18n";

/** A node of a ProseMirror document in Exciter's schema, read without trusting its shape. */
interface ExciterNode {
  type?: unknown;
  text?: unknown;
  attrs?: Record<string, unknown>;
  content?: unknown;
  marks?: unknown;
}

const LINK_PROTOCOLS = new Set(["http:", "https:", "mailto:"]);

function nodesOf(value: unknown): ExciterNode[] {
  return Array.isArray(value) ? value.filter((n): n is ExciterNode => typeof n === "object" && n !== null) : [];
}

function str(value: unknown): string {
  return typeof value === "string" ? value : "";
}

function positive(value: unknown): number | undefined {
  return typeof value === "number" && Number.isInteger(value) && value > 0 ? value : undefined;
}

/** An address a link may open: an absolute one of a protocol a reader can follow, never a script. */
export function safeHref(raw: unknown): string | null {
  if (typeof raw !== "string") return null;
  try {
    const url = new URL(raw);
    return LINK_PROTOCOLS.has(url.protocol) ? url.href : null;
  } catch {
    return null;
  }
}

/** The words of a node and everything in it, blocks apart by a space. */
export function textOf(node: ExciterNode): string {
  if (node.type === "text") return str(node.text);
  if (node.type === "hardBreak") return "\n";
  if (node.type === "mention") return `@${str(node.attrs?.label)}`;
  if (node.type === "attachment") return str(node.attrs?.fileName);
  if (node.type === "image") return str(node.attrs?.alt);
  const parts = nodesOf(node.content).map(textOf);
  const inline = nodesOf(node.content).every((c) => c.type === "text" || c.type === "hardBreak" || c.type === "mention" || c.type === "attachment");
  return parts.join(inline ? "" : " ").trim();
}

function marked(node: ExciterNode, links: boolean): ReactNode {
  let out: ReactNode = str(node.text);
  for (const mark of nodesOf(node.marks)) {
    switch (mark.type) {
      case "bold":
        out = <strong>{out}</strong>;
        break;
      case "italic":
        out = <em>{out}</em>;
        break;
      case "strike":
        out = <s>{out}</s>;
        break;
      case "code":
        out = <code>{out}</code>;
        break;
      case "link": {
        const href = safeHref(mark.attrs?.href);
        if (href && links) {
          out = (
            <a href={href} target="_blank" rel="noopener noreferrer nofollow">
              {out}
            </a>
          );
        }
        break;
      }
      // A mark Stator does not know, such as a glossary term, keeps its words.
    }
  }
  return out;
}

function inline(nodes: ExciterNode[], links: boolean): ReactNode[] {
  return nodes.map((node, i) => {
    switch (node.type) {
      case "text":
        return <span key={i}>{marked(node, links)}</span>;
      case "hardBreak":
        return <br key={i} />;
      default:
        return <span key={i}>{textOf(node)}</span>;
    }
  });
}

function cellSpan(value: unknown): number | undefined {
  const n = positive(value);
  return n && n > 1 ? n : undefined;
}

function block(node: ExciterNode, key: number, depth: number, links: boolean): ReactNode {
  const children = nodesOf(node.content);
  if (depth > EXCITER_DOCUMENT_MAX_DEPTH) {
    const text = textOf(node);
    return text ? <p key={key}>{text}</p> : null;
  }
  const inner = () => children.map((child, i) => block(child, i, depth + 1, links));
  switch (node.type) {
    case "paragraph":
      return <p key={key}>{inline(children, links)}</p>;
    // A heading of the requirement's would take a place in the page's outline.
    case "heading":
      return (
        <p key={key} className="font-semibold" data-exciter-heading={positive(node.attrs?.level) ?? 1}>
          {inline(children, links)}
        </p>
      );
    case "bulletList":
      return <ul key={key}>{inner()}</ul>;
    case "orderedList":
      return (
        <ol key={key} start={positive(node.attrs?.start)}>
          {inner()}
        </ol>
      );
    case "listItem":
      return <li key={key}>{inner()}</li>;
    case "blockquote":
      return <blockquote key={key}>{inner()}</blockquote>;
    case "codeBlock":
      return (
        <pre key={key}>
          <code>{textOf(node)}</code>
        </pre>
      );
    case "horizontalRule":
      return <hr key={key} />;
    case "table":
      return (
        <div key={key} className="doc-table-wrap">
          <table>
            <tbody>{inner()}</tbody>
          </table>
        </div>
      );
    case "tableRow":
      return <tr key={key}>{inner()}</tr>;
    case "tableCell":
    case "tableHeader": {
      const Cell = node.type === "tableHeader" ? "th" : "td";
      return (
        <Cell key={key} colSpan={cellSpan(node.attrs?.colspan)} rowSpan={cellSpan(node.attrs?.rowspan)}>
          {inner()}
        </Cell>
      );
    }
    // A picture is an attachment of Exciter's, which a reader's browser may not load from here.
    case "image":
      return (
        <p key={key} className="text-ink-muted" data-exciter-image="">
          {t.exciter.block.image(str(node.attrs?.alt))}
        </p>
      );
    default: {
      const text = textOf(node);
      return text ? (
        <p key={key} data-exciter-unknown={str(node.type)}>
          {text}
        </p>
      ) : null;
    }
  }
}

/** Whether a document has any words to show. */
export function hasContent(doc: unknown): boolean {
  return typeof doc === "object" && doc !== null && textOf(doc as ExciterNode) !== "";
}

/**
 * A statement or rationale as Exciter wrote it: the nodes and marks Stator
 * knows drawn as on a page, the rest as their words. Links open only on the page.
 */
export function ExciterDocument({ doc, links = true }: { doc: unknown; links?: boolean }) {
  if (typeof doc !== "object" || doc === null) return null;
  return <div className="space-y-2">{nodesOf((doc as ExciterNode).content).map((node, i) => block(node, i, 0, links))}</div>;
}
