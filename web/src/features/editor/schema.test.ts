import { describe, expect, it } from "vitest";
import { ANCHOR_PATTERN, FALLBACK_SLUG, dedupe, isEmptyDoc, safeHref, slug } from "./schema";

// The same cases as the API's TestSafeHref and TestSlug, so both sides agree.
describe("safeHref", () => {
  it("admits web and mail addresses and links within the site", () => {
    for (const ok of [
      "https://example.test/a?b=c#d",
      "http://example.test",
      "HTTPS://EXAMPLE.TEST",
      "mailto:ada@example.test",
      "/spaces/eng/pages/1",
      "#plan",
      "?q=1",
      "relative/page",
      "./here:colon",
      "../up",
    ]) {
      expect(safeHref(ok), ok).toBe(ok);
    }
  });

  it("refuses anything a browser would run or send elsewhere", () => {
    for (const bad of [
      "javascript:alert(1)",
      "JaVaScRiPt:alert(1)",
      "java\tscript:alert(1)",
      " javascript:alert(1)",
      "data:text/html,x",
      "file:///etc/passwd",
      "ftp://example.test",
      "mailto:",
      "https://",
      "//evil.test",
      "/\\evil.test",
      "https://ex ample.test",
      "a\u0000b",
      "foo:bar",
      "",
      42,
    ]) {
      expect(safeHref(bad), String(bad)).toBeNull();
    }
  });
});

describe("slug", () => {
  it("makes the anchors the API makes", () => {
    const cases: Array<[string, string]> = [
      ["Getting Started", "getting-started"],
      ["  What's new in 2.0?  ", "what-s-new-in-2-0"],
      ["Übersicht & Ziele", "übersicht-ziele"],
      ["!!!", FALLBACK_SLUG],
      ["", FALLBACK_SLUG],
      ["日本語の見出し", "日本語の見出し"],
      ["ab ".repeat(100), `${"ab-".repeat(21)}a`],
    ];
    for (const [text, want] of cases) {
      expect(slug(text), text).toBe(want);
      expect(ANCHOR_PATTERN.test(slug(text))).toBe(true);
    }
    expect(dedupe("a", new Set(["a", "a-2"]))).toBe("a-3");
  });
});

describe("isEmptyDoc", () => {
  it("is true only for a document that says nothing", () => {
    expect(isEmptyDoc(null)).toBe(true);
    expect(isEmptyDoc({ type: "doc", content: [{ type: "paragraph" }] })).toBe(true);
    expect(isEmptyDoc({ type: "doc", content: [{ type: "horizontalRule" }] })).toBe(false);
  });
});
