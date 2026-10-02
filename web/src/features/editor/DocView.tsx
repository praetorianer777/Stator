// biome-ignore-all lint/suspicious/noArrayIndexKey: a node has no identity but its place, and a read-only view never reorders them
import { Fragment, createContext, createElement, useContext, useEffect, useMemo, useRef, type MouseEvent, type ReactNode } from "react";
import type { Element as HastElement, ElementContent, Root } from "hast";
import { IconButton, cx } from "@/components/ui";
import { Icon } from "@/components/icons";
import { t } from "@/i18n";
import { DocAttachment, DocImage } from "./AttachmentView";
import { ChildPagesList, DocPageContext, TocList, childPagesSummary, tocSummary } from "./BlockViews";
import { childPagesOptions } from "./childPages";
import { buildToc, headingsOfDoc, tocMaxLevel, type FoundHeading } from "./toc";
import { useCopyHeadingLink } from "./CopyHeadingLink";
import { ExpandView, revealInExpands } from "./ExpandView";
import { languageLabel, lowlight } from "./languages";
import { ANCHOR_PATTERN, CELL_BACKGROUNDS, INLINE_COMMENT_MARK, PANEL_KINDS, safeHref, textOf, type DocNode } from "./schema";
import { Passage, usePassages, type BlockPath } from "./passages";
import { DATE_NODE, DateChip, STATUS_NODE, StatusLabel, isoDay, statusColor, statusLabel } from "./InlineValueViews";
import { ArmatureIssuesProvider, IssueChip } from "@/features/armature/IssueChip";
import { IssueBlock } from "@/features/armature/IssueBlock";
import { IssueList, listSettings } from "@/features/armature/IssueList";
import { DueChip } from "@/features/tasks/DueChip";
import { taskOfItem } from "@/features/tasks/taskItem";

import { ARMATURE_ISSUE_BLOCK_NODE, ARMATURE_ISSUE_LIST_NODE, ARMATURE_ISSUE_NODE, issueKeysOf, normalizeKey } from "@/features/armature/issueKeys";

/**
 * A document drawn as elements, never as HTML: every node becomes the React
 * element that means it, so nothing a person typed is ever parsed as markup
 * and an unsafe link is shown as text.
 */
export function DocView({
  doc,
  className,
  size = "base",
  anchors = true,
}: {
  doc: DocNode | null | undefined;
  className?: string;
  size?: "sm" | "base";
  /** False for a preview beside the page, whose headings must not take the page's anchors. */
  anchors?: boolean;
}) {
  const { copy, status } = useCopyHeadingLink();
  const headings = useMemo(() => headingsOfDoc(doc), [doc]);
  const keys = useMemo(() => issueKeysOf(doc), [doc]);
  const root = useRef<HTMLDivElement>(null);
  const shown = Boolean(doc);
  // The browser scrolled to the address's heading before the page was drawn,
  // and cannot reach one inside a closed expand block at all.
  useEffect(() => {
    if (!shown || !anchors) return;
    const anchor = anchorOfLocation();
    const target = anchor ? root.current?.querySelector(`[id="${CSS.escape(anchor)}"]`) : null;
    if (target && revealInExpands(target)) target.scrollIntoView?.({ block: "start" });
  }, [shown, anchors]);
  if (!doc) return null;
  return (
    <div ref={root} className={cx("doc-content", size === "sm" ? "text-sm" : "text-base", "text-ink", className)} data-doc>
      <HeadingsContext value={headings}>
        <WithIssues keys={keys}>
          <Blocks nodes={doc.content} copy={anchors ? copy : null} path={[]} />
        </WithIssues>
      </HeadingsContext>
      {status}
    </div>
  );
}

// A view that names no Armature issue asks nothing about Armature.
function WithIssues({ keys, children }: { keys: string[]; children: ReactNode }) {
  return keys.length > 0 ? <ArmatureIssuesProvider keys={keys}>{children}</ArmatureIssuesProvider> : children;
}

function anchorOfLocation(): string {
  try {
    return decodeURIComponent(window.location.hash.slice(1));
  } catch {
    return "";
  }
}

const HeadingsContext = createContext<FoundHeading[]>([]);

// The router is left out of it: the heading is on this page, so only the
// address's fragment changes, and nothing needs loading again.
function followHeading(anchor: string, event: MouseEvent<HTMLAnchorElement>) {
  const target = document.getElementById(anchor);
  if (!target) return;
  event.preventDefault();
  revealInExpands(target);
  target.scrollIntoView?.({ block: "start" });
  window.history.replaceState(window.history.state, "", `#${encodeURIComponent(anchor)}`);
}

function DocToc({ node }: { node: DocNode }) {
  const headings = useContext(HeadingsContext);
  return (
    <nav aria-label={t.editor.toc.label} data-toc className="doc-block">
      <TocList entries={buildToc(headings, tocMaxLevel(node.attrs?.maxLevel))} onFollow={followHeading} />
    </nav>
  );
}

/**
 * A comparison's blocks in reading order, each marked as kept, added, removed
 * or changed in words as well as in colour.
 */
export function DocDiffView({ blocks }: { blocks: { change: "equal" | "inserted" | "deleted" | "modified"; node: DocNode }[] }) {
  const labels = { inserted: t.history.added, deleted: t.history.removed, modified: t.history.changed };
  const keys = useMemo(() => issueKeysOf(blocks.map((block) => block.node)), [blocks]);
  return (
    <div className="doc-content text-base text-ink" data-doc data-diff-view>
      <WithIssues keys={keys}>
        {blocks.map((block, i) => (
          <div key={i} className="doc-diff-block" data-diff-block={block.change}>
            {block.change !== "equal" && <span className="doc-diff-label">{labels[block.change]}</span>}
            <Block node={block.node} copy={null} path={[i]} />
          </div>
        ))}
      </WithIssues>
    </div>
  );
}

// Null where headings get no anchors: a comparison can show one heading twice,
// and two elements with one id break its links and its accessibility.
type Copy = ((anchor: string) => void) | null;

function Blocks({ nodes, copy, path }: { nodes: DocNode[] | undefined; copy: Copy; path: BlockPath }) {
  return (
    <>
      {(nodes ?? []).map((node, i) => (
        <Block key={i} node={node} copy={copy} path={[...path, i]} />
      ))}
    </>
  );
}

const CELL_ALIGNS = ["left", "center", "right"] as const;

function oneOf<T extends string>(values: readonly T[], value: unknown): T | undefined {
  return typeof value === "string" && (values as readonly string[]).includes(value) ? (value as T) : undefined;
}

function Block({ node, copy, path }: { node: DocNode; copy: Copy; path: BlockPath }): ReactNode {
  const block = usePassages() ? path.join(".") : undefined;
  switch (node.type) {
    case "paragraph":
      return <p data-block={block}>{inline(node.content)}</p>;
    case "heading":
      return <Heading node={node} copy={copy} block={block} />;
    case "bulletList":
      return <ul>{items(node.content, copy, path)}</ul>;
    case "orderedList": {
      const start = Number(node.attrs?.start ?? 1);
      return <ol start={Number.isInteger(start) && start !== 1 ? start : undefined}>{items(node.content, copy, path)}</ol>;
    }
    case "taskList":
      return (
        <ul data-type="taskList">
          {(node.content ?? []).map((item, i) => (
            <TaskLine key={i} item={item} copy={copy} path={[...path, i]} />
          ))}
        </ul>
      );
    case "blockquote":
      return (
        <blockquote>
          <Blocks nodes={node.content} copy={copy} path={path} />
        </blockquote>
      );
    case "codeBlock":
      return <CodeBlock node={node} />;
    case "horizontalRule":
      return <hr />;
    case "table":
      return (
        <div className="doc-table-wrap">
          <table>
            <tbody>
              {(node.content ?? []).map((row, r) => (
                <tr key={r}>
                  {(row.content ?? []).map((cell, c) => {
                    const Tag = cell.type === "tableHeader" ? "th" : "td";
                    const colspan = Number(cell.attrs?.colspan ?? 1);
                    const rowspan = Number(cell.attrs?.rowspan ?? 1);
                    return (
                      <Tag
                        key={c}
                        colSpan={colspan > 1 ? colspan : undefined}
                        rowSpan={rowspan > 1 ? rowspan : undefined}
                        style={{ textAlign: oneOf(CELL_ALIGNS, cell.attrs?.align) }}
                        data-background={oneOf(CELL_BACKGROUNDS, cell.attrs?.background)}
                      >
                        <Blocks nodes={cell.content} copy={copy} path={[...path, r, c]} />
                      </Tag>
                    );
                  })}
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      );
    case "panel": {
      const kind = oneOf(PANEL_KINDS, node.attrs?.kind) ?? "info";
      return (
        <div role="note" data-panel={kind} aria-label={t.editor.panels[kind]}>
          <Blocks nodes={node.content} copy={copy} path={path} />
        </div>
      );
    }
    // A comparison or a preview shows everything, as a print does.
    case "expand":
      return (
        <ExpandView title={node.attrs?.title} initiallyOpen={!copy}>
          <Blocks nodes={node.content} copy={copy} path={path} />
        </ExpandView>
      );
    case "image":
      return <DocImage node={node} />;
    // A comparison says what the block asks for rather than drawing it: its
    // headings and pages are the page's now, not the version's.
    case "tableOfContents":
      if (!copy) {
        return (
          <p className="doc-block doc-block-summary" data-toc>
            {tocSummary(tocMaxLevel(node.attrs?.maxLevel))}
          </p>
        );
      }
      return <DocToc node={node} />;
    case "childPages": {
      const options = childPagesOptions(node.attrs);
      if (!copy) {
        return (
          <p className="doc-block doc-block-summary" data-child-pages>
            {childPagesSummary(options)}
          </p>
        );
      }
      return (
        <nav aria-label={t.editor.childPages.label} data-child-pages className="doc-block">
          <ChildPagesList options={options} />
        </nav>
      );
    }
    case ARMATURE_ISSUE_BLOCK_NODE: {
      const key = normalizeKey(node.attrs?.key);
      if (!key) return null;
      if (!copy) {
        return (
          <p className="doc-block doc-block-summary" data-armature-issue-block={key}>
            {t.armature.block.summary(key)}
          </p>
        );
      }
      return <IssueBlock issueKey={key} />;
    }
    // Its rows are each reader's own, now; a comparison says what it asks for.
    case ARMATURE_ISSUE_LIST_NODE: {
      const settings = listSettings(node.attrs);
      if (!settings.query.trim()) return null;
      if (!copy) {
        return (
          <p className="doc-block doc-block-summary" data-armature-issue-list="">
            {t.armature.list.summary(settings.query)}
          </p>
        );
      }
      return <IssueList settings={settings} />;
    }
    default:
      return <p>{textOf(node)}</p>;
  }
}

// A published item can be ticked off where the page offers it, which is the
// live page to its editors; a comparison or a preview draws the box still.
function TaskLine({ item, copy, path }: { item: DocNode; copy: Copy; path: BlockPath }) {
  const page = useContext(DocPageContext);
  const task = taskOfItem(item);
  const toggle = copy && task.id ? page?.toggleTask : undefined;
  const words = (item.content ?? [])
    .filter((block) => block.type !== "taskList")
    .map(textOf)
    .join(" ")
    .trim();
  return (
    <li data-checked={task.done} data-task-id={task.id ?? undefined}>
      <input
        type="checkbox"
        checked={task.done}
        readOnly={!toggle}
        disabled={!toggle}
        aria-label={words ? t.tasks.tick(words) : t.editor.taskDone}
        onChange={toggle && task.id ? (event) => toggle(task.id!, event.target.checked) : undefined}
        data-task-check={words}
      />
      <div>
        <Blocks nodes={item.content} copy={copy} path={path} />
      </div>
      <DueChip due={task.due} done={task.done} brief className="mt-1" />
    </li>
  );
}

function Heading({ node, copy, block }: { node: DocNode; copy: Copy; block?: string }) {
  const level = Math.min(Math.max(Number(node.attrs?.level ?? 1), 1), 3);
  const anchor = copy && typeof node.attrs?.id === "string" && ANCHOR_PATTERN.test(node.attrs.id) ? node.attrs.id : undefined;
  // The page's title is its h1, so a document's levels start one down.
  const tag = `h${level + 1}`;
  const text = textOf(node);
  return (
    <div className="doc-heading group" data-heading>
      {createElement(tag, { id: anchor, "data-level": level, "data-block": block }, inline(node.content))}
      {anchor && (
        <IconButton
          icon={<Icon.Hash />}
          label={t.editor.copyHeadingLinkTo(text)}
          size="xs"
          className="opacity-0 group-hover:opacity-100 focus-visible:opacity-100"
          onClick={() => copy?.(anchor)}
          data-copy-heading-link={anchor}
        />
      )}
    </div>
  );
}

function CodeBlock({ node }: { node: DocNode }) {
  const code = (node.content ?? []).map((n) => n.text ?? "").join("");
  const language = typeof node.attrs?.language === "string" && node.attrs.language ? node.attrs.language : undefined;
  let body: ReactNode = code;
  // Text a comparison marks keeps its marks, which highlighting would drop.
  if ((node.content ?? []).some((n) => n.marks?.length)) body = inline(node.content);
  else if (language && lowlight.registered(language)) body = hast(lowlight.highlight(language, code));
  return (
    <pre data-language={language}>
      {language && <span className="doc-code-language">{languageLabel(language)}</span>}
      <code className={language ? `language-${language}` : undefined}>{body}</code>
    </pre>
  );
}

/** The highlighter's tree as spans with its classes; its text stays text. */
function hast(node: Root | ElementContent): ReactNode {
  if (node.type === "text") return node.value;
  if (node.type === "element") {
    const el = node as HastElement;
    const className = Array.isArray(el.properties.className) ? el.properties.className.join(" ") : undefined;
    return createElement("span", { className }, ...el.children.map((child, i) => <Fragment key={i}>{hast(child)}</Fragment>));
  }
  if (node.type === "root") return node.children.map((child, i) => <Fragment key={i}>{hast(child as ElementContent)}</Fragment>);
  return null;
}

function items(nodes: DocNode[] | undefined, copy: Copy, path: BlockPath): ReactNode {
  return (nodes ?? []).map((item, i) => (
    <li key={i}>
      <Blocks nodes={item.content} copy={copy} path={[...path, i]} />
    </li>
  ));
}

function inline(nodes: DocNode[] | undefined): ReactNode {
  return (nodes ?? []).map((node, i) => <Fragment key={i}>{inlineNode(node)}</Fragment>);
}

function inlineNode(node: DocNode): ReactNode {
  switch (node.type) {
    case "text":
      return marked(node.text ?? "", node.marks);
    case "hardBreak":
      return <br />;
    case "mention":
      return marked(<span data-mention={String(node.attrs?.id ?? "")}>{`@${String(node.attrs?.label ?? "")}`}</span>, node.marks);
    case "attachment":
      return <DocAttachment node={node} />;
    case ARMATURE_ISSUE_NODE: {
      const key = normalizeKey(node.attrs?.key);
      // The chip is a link of its own, and a link inside a link is neither.
      return key
        ? marked(
            <IssueChip issueKey={key} />,
            node.marks?.filter((mark) => mark.type !== "link"),
          )
        : null;
    }
    case STATUS_NODE: {
      const label = statusLabel(node.attrs?.label);
      return label ? marked(<StatusLabel label={label} color={statusColor(node.attrs?.color)} />, node.marks) : null;
    }
    case DATE_NODE: {
      const day = isoDay(node.attrs?.date);
      return day ? marked(<DateChip day={day} />, node.marks) : null;
    }
    default:
      return textOf(node);
  }
}

/** Marks nest in the order they are listed; a link that is not a web, mail or site address is plain text. */
function marked(content: ReactNode, marks: DocNode["marks"]): ReactNode {
  let out: ReactNode = content;
  for (const mark of marks ?? []) {
    switch (mark.type) {
      case "bold":
        out = <strong>{out}</strong>;
        break;
      case "italic":
        out = <em>{out}</em>;
        break;
      case "code":
        out = <code>{out}</code>;
        break;
      case "strike":
        out = <s>{out}</s>;
        break;
      case INLINE_COMMENT_MARK:
        if (typeof mark.attrs?.threadId === "string") out = <Passage threadId={mark.attrs.threadId}>{out}</Passage>;
        break;
      case "hint":
        out = <span data-hint="">{out}</span>;
        break;
      // Only a comparison carries these two; a screen reader is told where
      // each starts and ends, since it announces neither element by itself.
      case "diffInsert":
        out = (
          <ins data-diff="insert">
            <span className="sr-only">{t.history.insertedStart}</span>
            {out}
            <span className="sr-only">{t.history.insertedEnd}</span>
          </ins>
        );
        break;
      case "diffDelete":
        out = (
          <del data-diff="delete">
            <span className="sr-only">{t.history.deletedStart}</span>
            {out}
            <span className="sr-only">{t.history.deletedEnd}</span>
          </del>
        );
        break;
      case "link": {
        const href = safeHref(mark.attrs?.href);
        if (href) {
          const external = /^(https?|mailto):/i.test(href);
          out = (
            <a href={href} rel={external ? "noopener noreferrer nofollow" : undefined} target={external ? "_blank" : undefined}>
              {out}
            </a>
          );
        }
        break;
      }
    }
  }
  return out;
}
