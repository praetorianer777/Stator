import { createContext, useContext } from "react";
import { LOGIN_PATH, PAGE_SLUG_FALLBACK, PUBLIC_LINK_SEGMENT, PUBLIC_PATH } from "@/config";

/** Whether a document is read in the public pages, by somebody who is not signed in; false inside the app. */
export const PublicReadingContext = createContext<boolean>(false);

export function usePublicReading(): boolean {
  return useContext(PublicReadingContext);
}

/** The token of the public link a page is read through, inside PublicReadingContext; null for every other reading. */
export const PublicLinkContext = createContext<string | null>(null);

export function usePublicLink(): string | null {
  return useContext(PublicLinkContext);
}

/** The id of the page read in the public pages, which its diagrams are drawn for; null for every other reading. */
export const PublicPageContext = createContext<string | null>(null);

export function usePublicPageId(): string | null {
  return useContext(PublicPageContext);
}

/** The reading view of the one page a public link opens. */
export function publicLinkPath(token: string): string {
  return `${PUBLIC_PATH}/${PUBLIC_LINK_SEGMENT}/${encodeURIComponent(token)}`;
}

const SPACE_ADDRESS = /^\/s\/([A-Za-z0-9]+)\/?$/;
const PAGE_ADDRESS = /^\/s\/([A-Za-z0-9]+)\/p\/([0-9a-f-]{36})(?:\/([^/]*))?\/?$/;

/** Where the public pages start. */
export function publicSitePath(): string {
  return PUBLIC_PATH;
}

export function publicSpacePath(spaceKey: string): string {
  return `${PUBLIC_PATH}/s/${spaceKey.toUpperCase()}`;
}

/** The public reading view of a page; without its space, the page is looked up and the address put right. */
export function publicPagePath(pageId: string, spaceKey?: string, slug?: string): string {
  if (!spaceKey) return `${PUBLIC_PATH}/p/${pageId}`;
  return `${publicSpacePath(spaceKey)}/p/${pageId}/${slug || PAGE_SLUG_FALLBACK}`;
}

/** Where signing in leads back to, from an address inside the app. */
export function signInPath(next: string): string {
  return `${LOGIN_PATH}?${new URLSearchParams({ next }).toString()}`;
}

/**
 * Where a link inside the app leads somebody who is not signed in: a page or
 * a space to its public reading view, which says so if it is not public, and
 * anything else to signing in. Another site's address is left alone.
 */
export function publicHref(href: string, origin: string = window.location.origin): string {
  let url: URL;
  try {
    url = new URL(href, origin);
  } catch {
    return href;
  }
  if (url.origin !== origin) return href;
  const page = PAGE_ADDRESS.exec(url.pathname);
  if (page) return publicPagePath(page[2] ?? "", page[1], page[3]) + url.hash;
  const space = SPACE_ADDRESS.exec(url.pathname);
  if (space?.[1]) return publicSpacePath(space[1]);
  if (url.pathname === PUBLIC_PATH || url.pathname.startsWith(`${PUBLIC_PATH}/`)) return url.pathname + url.search + url.hash;
  return signInPath(url.pathname + url.search + url.hash);
}
