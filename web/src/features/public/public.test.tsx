import { afterEach, describe, expect, it, vi } from "vitest";
import { render, screen, waitFor } from "@testing-library/react";
import { QueryClientProvider } from "@tanstack/react-query";
import { createMemoryHistory } from "@tanstack/react-router";
import { DocView } from "@/features/editor/DocView";
import { PUBLIC_LINK_SEGMENT } from "@/config";
import { PublicLinkContext, PublicReadingContext, publicHref, publicLinkPath, publicPagePath } from "@/features/editor/publicReading";
import { linkExpiry } from "@/features/sharing/PublicLinks";
import { publicLinkRoute } from "@/routes/public";
import type { Doc } from "@/features/editor/schema";
import type { PublicPage } from "@/api/public";
import { t } from "@/i18n";
import { LocalizedRouter } from "@/features/shell/LocalizedRouter";
import { createQueryClient } from "@/lib/session";
import { buildRouter, sendToLogin } from "@/routes";
import { stubApi } from "@/test/app";
import { privateAddress } from "./PublicScreens";

const PAGE = "0199a1b2-c3d4-7e5f-8a9b-0c1d2e3f4a01";
const FILE = "0199a1b2-c3d4-7e5f-8a9b-0c1d2e3f4a02";
const ORIGIN = "https://wiki.example.test";

describe("a link inside the app, read without signing in", () => {
  it("leads a page or a space to its public reading view, keeping the heading", () => {
    expect(publicHref(`/s/DOCS/p/${PAGE}/install#steps`, ORIGIN)).toBe(`/public/s/DOCS/p/${PAGE}/install#steps`);
    expect(publicHref(`${ORIGIN}/s/DOCS/p/${PAGE}`, ORIGIN)).toBe(`/public/s/DOCS/p/${PAGE}/page`);
    expect(publicHref("/s/docs", ORIGIN)).toBe("/public/s/DOCS");
  });

  it("leads anything else of the app to signing in, and leaves other sites alone", () => {
    expect(publicHref("/labels/howto", ORIGIN)).toBe("/login?next=%2Flabels%2Fhowto");
    expect(publicHref("https://example.org/x", ORIGIN)).toBe("https://example.org/x");
  });

  it("names a page by id alone where its space is not known", () => {
    expect(publicPagePath(PAGE)).toBe(`/public/p/${PAGE}`);
  });
});

describe("signing in from a public page", () => {
  const page = { id: PAGE, title: "Install it", home: false, space: { key: "DOCS" } } as PublicPage;

  it("comes back to the same page inside the app", () => {
    expect(privateAddress(`/public/s/DOCS/p/${PAGE}/install-it`, "", page)).toBe(`/s/DOCS/p/${PAGE}/install-it`);
    expect(privateAddress(`/public/s/DOCS/p/${PAGE}/install-it`, "", { ...page, home: true })).toBe("/s/DOCS");
    expect(privateAddress("/public/s/docs", "", undefined)).toBe("/s/DOCS");
    expect(privateAddress("/public/search", "?q=zebra", undefined)).toBe("/search?q=zebra");
    expect(privateAddress("/public", "", undefined)).toBe("/");
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
      <PublicReadingContext value={true}>
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
    expect(screen.getByRole("link", { name: "the guide" }).getAttribute("href")).toBe(`/public/s/DOCS/p/${PAGE}/guide`);
    expect(screen.getByRole("img", { name: "A map" }).getAttribute("src")).toBe(`/api/v1/public/attachments/${FILE}?inline=1`);
    expect(screen.getByRole("link", { name: t.publicReading.included }).getAttribute("href")).toBe(`/public/p/${PAGE}`);
  });

  it("says what a generated block lists rather than asking as a reader", () => {
    readAnonymously();
    expect(screen.getByText(t.contributors.title("page"))).toBeTruthy();
  });

  it("loads its files through the link it is read through, and links other pages to the public view", () => {
    render(
      <PublicReadingContext value={true}>
        <PublicLinkContext value="t0k3n">
          <DocView doc={doc} />
        </PublicLinkContext>
      </PublicReadingContext>,
    );
    expect(screen.getByRole("img", { name: "A map" }).getAttribute("src")).toBe(`/api/v1/public/links/t0k3n/attachments/${FILE}?inline=1`);
    expect(screen.getByRole("link", { name: "the guide" }).getAttribute("href")).toBe(`/public/s/DOCS/p/${PAGE}/guide`);
  });
});

describe("an address from when the public pages named the organization", () => {
  afterEach(() => vi.unstubAllGlobals());

  async function open(path: string) {
    const queryClient = createQueryClient((client) => sendToLogin(router, client));
    const router = buildRouter(queryClient, createMemoryHistory({ initialEntries: [path] }));
    await router.load();
    render(
      <QueryClientProvider client={queryClient}>
        <LocalizedRouter router={router} />
      </QueryClientProvider>,
    );
    return router;
  }

  it("goes on to the same page, query and all, when the API says the slug is this organization's", async () => {
    const sent = stubApi({
      "GET /public/acme": { status: 308 },
      "GET /public": { status: 200, body: { site: { name: "Acme", indexable: false, footer: { en: "", de: "" }, logoVersion: null }, spaces: [] } },
      "GET /public/search": { status: 200, body: { hits: [], more: false, limit: 20, offset: 0 } },
    });
    const router = await open("/public/acme/search?q=zebra");
    await waitFor(() => expect(router.state.location.pathname).toBe("/public/search"));
    expect(router.state.location.search).toEqual({ q: "zebra" });
    expect(sent.some((r) => r.path === "/public/acme")).toBe(true);
  });

  it("is not found when the slug is another organization's", async () => {
    stubApi({ "GET /public/globex": { status: 404, body: { error: { code: "not_public", message: "That is not open to read without signing in." } } } });
    const router = await open("/public/globex/s/DOCS");
    expect(await screen.findByText(t.publicReading.notPublicTitle)).toBeInTheDocument();
    expect(router.state.location.pathname).toBe("/public/globex/s/DOCS");
  });
});

describe("a public link", () => {
  it("names the token under the public pages, as the route and the API do", () => {
    expect(publicLinkPath("abc_DEF-123")).toBe("/public/link/abc_DEF-123");
    expect((publicLinkRoute.options as { path: string }).path).toBe(`/public/${PUBLIC_LINK_SEGMENT}/$token`);
  });

  it("runs out the days asked for from now, or never", () => {
    const now = new Date("2026-10-06T12:00:00Z");
    expect(linkExpiry(7, now)).toBe("2026-10-13T12:00:00.000Z");
    expect(linkExpiry(0, now)).toBeUndefined();
  });
});
