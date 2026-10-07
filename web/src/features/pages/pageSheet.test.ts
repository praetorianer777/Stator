import { describe, expect, it } from "vitest";
import { PAGE_MAX_WIDTH_REM, PAGE_MEASURE_REM } from "@/config";
import { pageSheet } from "./pageSheet";

describe("pageSheet", () => {
  it("holds text to the measure and wide blocks to the maximum at the fixed width", () => {
    expect(pageSheet("fixed").style).toEqual({ "--page-measure": `${PAGE_MEASURE_REM}rem`, "--page-max-width": `${PAGE_MAX_WIDTH_REM}rem` });
  });

  it("lets text and wide blocks alike take the window at Full width", () => {
    expect(pageSheet("full").style).toEqual({ "--page-measure": "100%", "--page-max-width": "none" });
  });

  it("keeps the measure narrower than the maximum, or nothing would break out", () => {
    expect(PAGE_MEASURE_REM).toBeLessThan(PAGE_MAX_WIDTH_REM);
  });
});
