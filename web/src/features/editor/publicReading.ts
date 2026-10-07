import { createContext, useContext } from "react";
import { LOGIN_PATH, PAGE_SLUG_FALLBACK, PUBLIC_LINK_SEGMENT, PUBLIC_PATH } from "@/config";

/** The organization whose public pages a document is read in, by somebody who is not signed in; null inside the app. */
export const PublicReadingContext = createContext<string | null>(null);

export function usePublicReading(): string | null {
  return useContext(PublicReadingContext);
}

/** The token of the public link a page is read through, inside PublicReadingContext; null for every other reading. */
export const PublicLinkContext = createContext<string | null>(null);

export function usePublicLink(): string | null {
  return useContext(PublicLinkContext);
}

/** The reading view of the one page a public link opens. */
export function publicLinkPath(org: string, token: string): string {
  return `${publicSitePath(org)}/${PUBLIC_LINK_SEGMENT}/${encodeURIComponent(token)}`;
}

const SPACE_ADDRESS = /^\/s\/([A-Za-z0-9]+)\/?$/;
const PAGE_ADDRESS = /^\/s\/([A-Za-z0-9]+)\/p\/([0-9a-f-]{36})(?:\/([^/]*))?\/?$/;

/** Where the public pages of an organization start. */
export function publicSitePath(org: string): string {
  return `${PUBLIC_PATH}/${encodeURIComponent(org)}`;
}

export function publicSpacePath(org: string, spaceKey: string): string {
  return `${publicSitePath(org)}/s/${spaceKey.toUpperCase()}`;
}

/** The public reading view of a page; without its space, the page is looked up and the address put right. */
export function publicPagePath(org: string, pageId: string, spaceKey?: string, slug?: string): string {
  if (!spaceKey) return `${publicSitePath(org)}/p/${pageId}`;
  return `${publicSpacePath(org, spaceKey)}/p/${pageId}/${slug || PAGE_SLUG_FALLBACK}`;
}

/** Where signing in leads back to, from an address inside the app. */
export function signInPath(next: string, org?: string): string {
  const search = new URLSearchParams({ next });
  if (org) search.set("org", org);
  return `${LOGIN_PATH}?${search.toString()}`;
}

/**
 * Where a link inside the app leads somebody who is not signed in: a page or
 * a space to its public reading view, which says so if it is not public, and
 * anything else to signing in. Another site's address is left alone.
 */
export function publicHref(org: string, href: string, origin: string = window.location.origin): string {
  let url: URL;
  try {
    url = new URL(href, origin);
  } catch {
    return href;
  }
  if (url.origin !== origin) return href;
  const page = PAGE_ADDRESS.exec(url.pathname);
  if (page) return publicPagePath(org, page[2] ?? "", page[1], page[3]) + url.hash;
  const space = SPACE_ADDRESS.exec(url.pathname);
  if (space?.[1]) return publicSpacePath(org, space[1]);
  if (url.pathname.startsWith(`${PUBLIC_PATH}/`)) return url.pathname + url.search + url.hash;
  return signInPath(url.pathname + url.search + url.hash, org);
}
