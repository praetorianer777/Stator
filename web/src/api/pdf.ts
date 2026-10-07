import { API_BASE } from "@/config";
import { t } from "@/i18n";
import { ApiError, type ApiErrorBody } from "./client";

/** Where a page's PDF is printed for the signed-in reader. */
export function pagePdfHref(pageId: string): string {
  return `${API_BASE}/pages/${encodeURIComponent(pageId)}/pdf`;
}

/** Where a page anybody may read is printed. */
export function publicPdfHref(org: string, pageId: string): string {
  return `${API_BASE}/public/${encodeURIComponent(org)}/pages/${encodeURIComponent(pageId)}/pdf`;
}

/** Where the page a public link opens is printed, for whoever holds the link. */
export function linkedPdfHref(org: string, token: string): string {
  return `${API_BASE}/public/${encodeURIComponent(org)}/links/${encodeURIComponent(token)}/pdf`;
}

/** A printed page and the name the API gave it. */
export interface PrintedPdf {
  blob: Blob;
  name: string;
}

const FILENAME = /filename\*?=(?:UTF-8'')?"?([^";]+)"?/i;

/** The file name a Content-Disposition header gives, else the fallback. */
export function fileNameOf(disposition: string | null, fallback: string): string {
  const found = disposition ? FILENAME.exec(disposition)?.[1] : undefined;
  if (!found) return fallback;
  try {
    return decodeURIComponent(found);
  } catch {
    return found;
  }
}

/**
 * Fetches a PDF rather than letting the browser follow a link, since a print
 * takes seconds and a refusal has to be read as a sentence, not saved as a file.
 */
export async function fetchPdf(href: string, fallbackName: string): Promise<PrintedPdf> {
  let response: Response;
  try {
    response = await globalThis.fetch(new Request(new URL(href, window.location.origin), { credentials: "same-origin" }));
  } catch {
    throw new ApiError(0, { code: "network", message: t.pdf.networkFailed });
  }
  if (!response.ok) {
    let body: { error?: ApiErrorBody } | undefined;
    try {
      body = (await response.json()) as { error?: ApiErrorBody };
    } catch {
      body = undefined;
    }
    throw new ApiError(response.status, body?.error ?? { code: "unexpected_response", message: t.api.unexpected(response.status) });
  }
  return { blob: await response.blob(), name: fileNameOf(response.headers.get("Content-Disposition"), fallbackName) };
}

/** Hands a fetched file to the browser to save under its name. */
export function saveFile({ blob, name }: PrintedPdf): void {
  const url = URL.createObjectURL(blob);
  const link = document.createElement("a");
  link.href = url;
  link.download = name;
  document.body.append(link);
  link.click();
  link.remove();
  // A moment later, so the browser has taken the file before its address goes.
  setTimeout(() => URL.revokeObjectURL(url), 0);
}
