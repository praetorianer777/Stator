import { describe, expect, it } from "vitest";
import { PAGE_SLUG_MAX_LENGTH } from "@/config";
import { pageSlug } from "./slug";

describe("a page's slug", () => {
  it("is the title in lower case with words joined by hyphens", () => {
    expect(pageSlug("Getting Started")).toBe("getting-started");
    expect(pageSlug("  How we work: the handbook!  ")).toBe("how-we-work-the-handbook");
    expect(pageSlug("Über Größen")).toBe("über-größen");
  });

  it("falls back to a word when the title leaves nothing", () => {
    expect(pageSlug("")).toBe("page");
    expect(pageSlug("!!!")).toBe("page");
  });

  it("is cut short, never on a hyphen", () => {
    const slug = pageSlug("word ".repeat(40));
    expect([...slug].length).toBeLessThanOrEqual(PAGE_SLUG_MAX_LENGTH);
    expect(slug.endsWith("-")).toBe(false);
  });
});
