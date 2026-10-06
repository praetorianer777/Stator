import { describe, expect, it } from "vitest";
import { render, screen } from "@testing-library/react";
import { DocView } from "@/features/editor/DocView";
import { PublicReadingContext, publicHref, publicPagePath } from "@/features/editor/publicReading";
import type { Doc } from "@/features/editor/schema";
import type { PublicPage } from "@/api/public";
import { t } from "@/i18n";
import { privateAddress } from "./PublicScreens";

const PAGE = "0199a1b2-c3d4-7e5f-8a9b-0c1d2e3f4a01";
const FILE = "0199a1b2-c3d4-7e5f-8a9b-0c1d2e3f4a02";
const ORIGIN = "https://wiki.example.test";

describe("a link inside the app, read without signing in", () => {
  it("leads a page or a space to its public reading view, keeping the heading", () => {
    expect(publicHref("acme", `/s/DOCS/p/${PAGE}/install#steps`, ORIGIN)).toBe(`/public/acme/s/DOCS/p/${PAGE}/install#steps`);
    expect(publicHref("acme", `${ORIGIN}/s/DOCS/p/${PAGE}`, ORIGIN)).toBe(`/public/acme/s/DOCS/p/${PAGE}/page`);
    expect(publicHref("acme", "/s/docs", ORIGIN)).toBe("/public/acme/s/DOCS");
  });

  it("leads anything else of the app to signing in, and leaves other sites alone", () => {
    expect(publicHref("acme", "/labels/howto", ORIGIN)).toBe("/login?next=%2Flabels%2Fhowto&org=acme");
    expect(publicHref("acme", "https://example.org/x", ORIGIN)).toBe("https://example.org/x");
  });

  it("names a page by id alone where its space is not known", () => {
    expect(publicPagePath("acme", PAGE)).toBe(`/public/acme/p/${PAGE}`);
  });
});

describe("signing in from a public page", () => {
  const page = { id: PAGE, title: "Install it", home: false, space: { key: "DOCS" } } as PublicPage;

  it("comes back to the same page inside the app", () => {
    expect(privateAddress(`/public/acme/s/DOCS/p/${PAGE}/install-it`, "", page)).toBe(`/s/DOCS/p/${PAGE}/install-it`);
    expect(privateAddress(`/public/acme/s/DOCS/p/${PAGE}/install-it`, "", { ...page, home: true })).toBe("/s/DOCS");
    expect(privateAddress("/public/acme/s/docs", "", undefined)).toBe("/s/DOCS");
    expect(privateAddress("/public/acme/search", "?q=zebra", undefined)).toBe("/search?q=zebra");
    expect(privateAddress("/public/acme", "", undefined)).toBe("/");
  });
});

describe("a document read without signing in", () => {
  const doc: Doc = {
    type: "doc",
    content: [
      {
        type: "paragraph",
        content: [
          { type: "text", text: "Ask " },
          { type: "mention" },
          { type: "text", text: " or read ", marks: [] },
          { type: "text", text: "the guide", marks: [{ type: "link", attrs: { href: `/s/DOCS/p/${PAGE}/guide` } }] },
        ],
      },
      { type: "image", attrs: { attachmentId: FILE, alt: "A map" } },
      { type: "contributors", attrs: { scope: "page", limit: 10 } },
      { type: "include", attrs: { pageId: PAGE } },
    ],
  };

  function readAnonymously() {
    return render(
      <PublicReadingContext value="acme">
        <DocView doc={doc} />
      </PublicReadingContext>,
    );
  }

  it("names nobody it mentions", () => {
    const { container } = readAnonymously();
    expect(container.querySelector("[data-mention]")?.textContent).toBe(`@${t.publicReading.someone}`);
  });

  it("links pages and files through the public reads", () => {
    readAnonymously();
    expect(screen.getByRole("link", { name: "the guide" }).getAttribute("href")).toBe(`/public/acme/s/DOCS/p/${PAGE}/guide`);
    expect(screen.getByRole("img", { name: "A map" }).getAttribute("src")).toBe(`/api/v1/public/acme/attachments/${FILE}?inline=1`);
    expect(screen.getByRole("link", { name: t.publicReading.included }).getAttribute("href")).toBe(`/public/acme/p/${PAGE}`);
  });

  it("says what a generated block lists rather than asking as a reader", () => {
    readAnonymously();
    expect(screen.getByText(t.contributors.title("page"))).toBeTruthy();
  });
});
