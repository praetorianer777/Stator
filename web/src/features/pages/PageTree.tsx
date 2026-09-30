import { createContext, useContext, useId, useRef, useState, type DragEvent, type KeyboardEvent } from "react";
import { useNavigate } from "@tanstack/react-router";
import type { Space } from "@/api/spaces";
import { useChildren, useMovePage, type Placement, type TreeNode } from "@/api/tree";
import { cx } from "@/components/ui";
import { Icon } from "@/components/icons";
import { TREE_DROP_EDGE, TREE_INDENT_PX } from "@/config";
import { t } from "@/i18n";
import { pageSlug } from "@/lib/slug";
import { PageLink } from "./PageLink";

/** Where a dragged page lands against the row under the pointer. */
export type DropZone = "before" | "inside" | "after";

/** The zone of a row a pointer at offsetY of height is over: its edges place beside, its middle inside. */
export function dropZone(offsetY: number, height: number): DropZone {
  if (offsetY < height * TREE_DROP_EDGE) return "before";
  if (offsetY > height * (1 - TREE_DROP_EDGE)) return "after";
  return "inside";
}

/** What a drop in a zone of a row asks the API for. */
export function dropPlacement(node: TreeNode, zone: DropZone): Placement {
  if (zone === "inside") return { parentId: node.id };
  return zone === "before" ? { parentId: node.parentId, beforeId: node.id } : { parentId: node.parentId, afterId: node.id };
}

interface TreeState {
  space: Space;
  currentId?: string;
  roving?: string;
  expanded: Set<string>;
  toggle: (id: string, open?: boolean) => void;
  focus: (el: Element | null | undefined) => void;
  setFocused: (id: string) => void;
  onMove: (node: TreeNode) => void;
  drag: {
    from: { current: HTMLElement | null };
    over?: { id: string; zone: DropZone };
    setOver: (over?: { id: string; zone: DropZone }) => void;
    drop: (id: string, placement: Placement) => void;
  };
}

const TreeContext = createContext<TreeState | null>(null);

/**
 * A space's pages as a tree, a level loaded as it opens. Arrow keys walk it
 * as a tree does, Enter opens a page and M moves it; a page dragged onto the
 * edge of a row goes beside it, onto its middle under it.
 */
export function PageTree({ space, currentId, openPath, onMove }: { space: Space; currentId?: string; openPath: string[]; onMove: (node: TreeNode) => void }) {
  const [expanded, setExpanded] = useState(() => new Set(openPath));
  // The path to the page being read opens as the reader arrives there, and
  // stays as they left it otherwise; adjusted during render, not after.
  const pathKey = openPath.join("/");
  const [seenPath, setSeenPath] = useState(pathKey);
  if (seenPath !== pathKey) {
    setSeenPath(pathKey);
    setExpanded((before) => new Set([...before, ...openPath]));
  }
  const [focused, setFocused] = useState<string>();
  const [over, setOver] = useState<{ id: string; zone: DropZone }>();
  const [status, setStatus] = useState("");
  const from = useRef<HTMLElement | null>(null);
  const hintId = useId();
  const top = useChildren(space.key);
  const move = useMovePage();

  const toggle = (id: string, open?: boolean) =>
    setExpanded((before) => {
      const next = new Set(before);
      if (open ?? !next.has(id)) next.add(id);
      else next.delete(id);
      return next;
    });
  const focus = (el: Element | null | undefined) => {
    if (!(el instanceof HTMLElement) || !el.dataset.treeItem) return;
    setFocused(el.dataset.treeItem);
    el.focus();
  };
  const drop = (id: string, placement: Placement) => {
    const title = from.current?.dataset.treeTitle ?? "";
    if (placement.parentId && !placement.beforeId && !placement.afterId) toggle(placement.parentId, true);
    setStatus("");
    move.mutate({ id, ...placement }, { onSuccess: () => setStatus(t.tree.moved(title)), onError: (error) => setStatus(error.message) });
  };

  const onCurrent = currentId && currentId !== space.homePageId ? currentId : undefined;
  const state: TreeState = {
    space,
    currentId,
    roving: focused ?? onCurrent ?? top.data?.[0]?.id,
    expanded,
    toggle,
    focus,
    setFocused,
    onMove,
    drag: { from, over, setOver, drop },
  };

  if (top.data && top.data.length === 0) return <p className="px-2 py-1 text-sm text-ink-subtle">{t.tree.empty}</p>;
  return (
    <TreeContext.Provider value={state}>
      <div role="tree" aria-label={t.tree.label(space.name)} aria-describedby={hintId} aria-busy={top.isLoading} data-page-tree={space.key}>
        <Level level={1} />
      </div>
      <p id={hintId} className="sr-only">
        {t.tree.keys}
      </p>
      <p role="status" className="px-2 text-xs text-ink-muted [&:not(:empty)]:py-1" data-tree-status>
        {status}
      </p>
    </TreeContext.Provider>
  );
}

function useTree(): TreeState {
  const tree = useContext(TreeContext);
  if (!tree) throw new Error("a tree item is drawn outside its tree");
  return tree;
}

function Level({ parentId, level }: { parentId?: string; level: number }) {
  const tree = useTree();
  const { data } = useChildren(tree.space.key, parentId);
  return (
    <>
      {(data ?? []).map((node) => (
        <Item key={node.id} node={node} level={level} />
      ))}
    </>
  );
}

function Item({ node, level }: { node: TreeNode; level: number }) {
  const tree = useTree();
  const navigate = useNavigate();
  const open = node.hasChildren && tree.expanded.has(node.id);
  const current = node.id === tree.currentId;
  // The tree does not know each page's edit lists; the API refuses a move past one, and the tree says so.
  const movable = tree.space.can.editPages;
  const over = tree.drag.over?.id === node.id ? tree.drag.over.zone : undefined;

  function onKeyDown(event: KeyboardEvent<HTMLDivElement>) {
    if (event.target !== event.currentTarget || event.altKey || event.ctrlKey || event.metaKey) return;
    const item = event.currentTarget;
    const items = Array.from(item.closest('[role="tree"]')?.querySelectorAll('[role="treeitem"]') ?? []);
    const index = items.indexOf(item);
    const handled = () => {
      event.preventDefault();
      event.stopPropagation();
    };
    switch (event.key) {
      case "ArrowDown":
        handled();
        tree.focus(items[index + 1]);
        break;
      case "ArrowUp":
        handled();
        tree.focus(items[index - 1]);
        break;
      case "Home":
        handled();
        tree.focus(items[0]);
        break;
      case "End":
        handled();
        tree.focus(items[items.length - 1]);
        break;
      case "ArrowRight":
        handled();
        if (node.hasChildren && !open) tree.toggle(node.id, true);
        else if (open) tree.focus(item.querySelector('[role="group"] > [role="treeitem"]'));
        break;
      case "ArrowLeft":
        handled();
        if (open) tree.toggle(node.id, false);
        else tree.focus(item.parentElement?.closest('[role="treeitem"]'));
        break;
      case "Enter":
        handled();
        void navigate({ to: "/s/$spaceKey/p/$pageId/$slug", params: { spaceKey: tree.space.key, pageId: node.id, slug: pageSlug(node.title) } });
        break;
      case "m":
      case "M":
        if (!movable) break;
        handled();
        tree.onMove(node);
        break;
    }
  }

  function onDragStart(event: DragEvent<HTMLDivElement>) {
    event.stopPropagation();
    event.dataTransfer.setData("text/plain", node.title);
    event.dataTransfer.effectAllowed = "move";
    tree.drag.from.current = event.currentTarget;
  }

  function onDragOver(event: DragEvent<HTMLDivElement>) {
    const from = tree.drag.from.current;
    // Onto itself or anything below it would cut the page off from its space.
    if (!from || from.contains(event.currentTarget)) return;
    event.preventDefault();
    event.stopPropagation();
    event.dataTransfer.dropEffect = "move";
    const rect = event.currentTarget.getBoundingClientRect();
    const zone = dropZone(event.clientY - rect.top, rect.height);
    if (tree.drag.over?.id !== node.id || tree.drag.over.zone !== zone) tree.drag.setOver({ id: node.id, zone });
  }

  function onDrop(event: DragEvent<HTMLDivElement>) {
    const from = tree.drag.from.current;
    event.preventDefault();
    event.stopPropagation();
    tree.drag.setOver(undefined);
    if (!from?.dataset.treeItem || from.contains(event.currentTarget)) return;
    const rect = event.currentTarget.getBoundingClientRect();
    tree.drag.drop(from.dataset.treeItem, dropPlacement(node, dropZone(event.clientY - rect.top, rect.height)));
  }

  return (
    <div
      role="treeitem"
      aria-label={node.title}
      aria-level={level}
      aria-expanded={node.hasChildren ? open : undefined}
      aria-selected={current}
      aria-keyshortcuts={movable ? "M" : undefined}
      tabIndex={tree.roving === node.id ? 0 : -1}
      draggable={movable}
      onDragStart={onDragStart}
      onDragEnd={() => {
        tree.drag.from.current = null;
        tree.drag.setOver(undefined);
      }}
      onKeyDown={onKeyDown}
      onFocus={(event) => event.target === event.currentTarget && tree.setFocused(node.id)}
      data-tree-item={node.id}
      data-tree-title={node.title}
      className="outline-none [&:focus-visible>[data-tree-row]]:ring-2 [&:focus-visible>[data-tree-row]]:ring-focus"
    >
      {/* biome-ignore lint/a11y/noStaticElementInteractions: a dragged page lands on the row; the keyboard moves pages with M */}
      <div
        className={cx(
          "relative flex h-8 items-center gap-1 rounded-control pr-2 text-sm text-ink-muted hover:bg-surface-raised hover:text-ink",
          current && "bg-accent-subtle font-medium text-accent",
          over === "inside" && "ring-2 ring-accent",
          over === "before" && "before:absolute before:inset-x-0 before:top-0 before:h-0.5 before:bg-accent",
          over === "after" && "after:absolute after:inset-x-0 after:bottom-0 after:h-0.5 after:bg-accent",
        )}
        style={{ paddingLeft: (level - 1) * TREE_INDENT_PX }}
        onDragOver={onDragOver}
        onDragLeave={() => tree.drag.over?.id === node.id && tree.drag.setOver(undefined)}
        onDrop={onDrop}
        data-tree-row
        data-drop={over}
      >
        {/* For the pointer only: the keyboard opens and closes with the arrows. */}
        <span
          aria-hidden="true"
          className="flex size-5 shrink-0 cursor-pointer items-center justify-center"
          onClick={() => node.hasChildren && tree.toggle(node.id)}
        >
          {node.hasChildren && <Icon.ChevronDown className={cx("transition-transform", !open && "-rotate-90")} />}
        </span>
        <PageLink spaceKey={tree.space.key} id={node.id} title={node.title} tabIndex={-1} className="min-w-0 flex-1 truncate">
          {node.title}
        </PageLink>
        {node.restricted && (
          <span className="flex shrink-0 items-center text-ink-subtle" data-tree-restricted>
            <Icon.Lock />
            <span className="sr-only">{t.restrictions.treeView}</span>
          </span>
        )}
      </div>
      {open && (
        <div role="group">
          <Level parentId={node.id} level={level + 1} />
        </div>
      )}
    </div>
  );
}
