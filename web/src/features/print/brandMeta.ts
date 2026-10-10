import { useEffect, useState } from "react";
import { brandLogoHref, useBrand } from "@/api/brand";
import { API_BASE, PRINT_LOGO_HEIGHT_PX } from "@/config";
import { language, type Language } from "@/i18n";

/** What the organization's brand adds to a printed sheet: its logo as a picture the running header can hold, and its footer line. */
export interface PrintBrand {
  logo: string;
  footer: string;
}

/** A footer line in the reader's language, the English one where that language has none. */
export function footerLine(footer: { en: string; de: string }, lang: Language = language()): string {
  return lang === "de" && footer.de ? footer.de : footer.en;
}

/** Where a public site, or a public link, serves the organization's logo from. */
export function publicLogoHref(org: string, logoVersion: number | null, token?: string): string | null {
  if (logoVersion === null) return null;
  const base = token ? `${API_BASE}/public/${org}/links/${token}` : `${API_BASE}/public/${org}`;
  return `${base}/logo?v=${logoVersion}`;
}

// A running header is drawn outside the page, where the browser loads no
// address, so the logo goes in as a small picture of its own.
async function headerPicture(href: string): Promise<string> {
  const response = await fetch(href);
  if (!response.ok) throw new Error(`the logo answered ${response.status}`);
  const bitmap = await createImageBitmap(await response.blob());
  const scale = Math.min(1, PRINT_LOGO_HEIGHT_PX / bitmap.height);
  const canvas = document.createElement("canvas");
  canvas.width = Math.max(1, Math.round(bitmap.width * scale));
  canvas.height = Math.max(1, Math.round(bitmap.height * scale));
  canvas.getContext("2d")?.drawImage(bitmap, 0, 0, canvas.width, canvas.height);
  return canvas.toDataURL("image/png");
}

/**
 * The brand for a print, or undefined while it is still being fetched. A logo
 * that cannot be fetched is left out: the sheet still prints, with the name.
 */
export function useBrandPicture(href: string | null, footer: string, known: boolean): PrintBrand | undefined {
  const [logo, setLogo] = useState<string | null | undefined>(href === null ? "" : undefined);
  useEffect(() => {
    if (!known) return;
    if (href === null) {
      setLogo("");
      return;
    }
    let live = true;
    headerPicture(href).then(
      (picture) => live && setLogo(picture),
      () => live && setLogo(""),
    );
    return () => {
      live = false;
    };
  }, [href, known]);
  if (!known || logo === undefined || logo === null) return undefined;
  return { logo, footer };
}

/** The brand of the signed-in reader's organization. */
export function useOrgPrintBrand(): PrintBrand | undefined {
  const { data, error } = useBrand();
  const href = data ? brandLogoHref(data) : null;
  const picture = useBrandPicture(href, data ? footerLine(data.footer) : "", Boolean(data));
  // A brand that cannot be read does not stop the print.
  return error ? { logo: "", footer: "" } : picture;
}
