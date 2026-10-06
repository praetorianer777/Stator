import type { CSSProperties } from "react";
import { PAGE_MAX_WIDTH_REM, PAGE_MEASURE_REM } from "@/config";

/** A page header's class, for a header the shell draws outside the sheet; it takes the sheet's style too. */
export const PAGE_SHEET_HEADER = "page-sheet-header";

/** How a page's sheet is laid out: text at the measure, wide blocks out to the maximum, or both to the window at Full width. */
export function pageSheet(width: "fixed" | "full"): { className: string; style: CSSProperties } {
  // Full width keeps a measure of 100% rather than none, so a picture
  // narrower than the window still starts where the text does.
  const style =
    width === "full"
      ? { "--page-measure": "100%", "--page-max-width": "none" }
      : { "--page-measure": `${PAGE_MEASURE_REM}rem`, "--page-max-width": `${PAGE_MAX_WIDTH_REM}rem` };
  return { className: "page-sheet", style: style as CSSProperties };
}
