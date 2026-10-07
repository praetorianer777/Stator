import { API_BASE } from "@/config";

/** Where a page's Word document is made for the signed-in reader. */
export function pageWordHref(pageId: string): string {
  return `${API_BASE}/pages/${encodeURIComponent(pageId)}/docx`;
}

/** Where a page anybody may read is made into a Word document. */
export function publicWordHref(org: string, pageId: string): string {
  return `${API_BASE}/public/${encodeURIComponent(org)}/pages/${encodeURIComponent(pageId)}/docx`;
}

/** Where the page a public link opens is made into a Word document, for whoever holds the link. */
export function linkedWordHref(org: string, token: string): string {
  return `${API_BASE}/public/${encodeURIComponent(org)}/links/${encodeURIComponent(token)}/docx`;
}
