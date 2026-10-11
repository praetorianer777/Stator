import type { ReactNode } from "react";
import { Link } from "@tanstack/react-router";
import { pageSlug } from "@/lib/slug";

/** A link to a page by its space and id, with its slug; the home page is its space's own address. */
export function PageLink({
  spaceKey,
  id,
  title,
  home = false,
  hash,
  className,
  tabIndex,
  onClick,
  children,
}: {
  spaceKey: string;
  id: string;
  title: string;
  home?: boolean;
  /** The fragment to open the page at, such as a task's anchor. */
  hash?: string;
  className?: string;
  tabIndex?: number;
  /** Called when the link is followed, such as to close the drawer it sits in. */
  onClick?: () => void;
  children?: ReactNode;
}) {
  if (home) {
    return (
      <Link to="/s/$spaceKey" params={{ spaceKey }} className={className} tabIndex={tabIndex} onClick={onClick}>
        {children ?? title}
      </Link>
    );
  }
  return (
    <Link
      to="/s/$spaceKey/p/$pageId/$slug"
      params={{ spaceKey, pageId: id, slug: pageSlug(title) }}
      hash={hash}
      className={className}
      tabIndex={tabIndex}
      onClick={onClick}
      draggable={false}
    >
      {children ?? title}
    </Link>
  );
}
