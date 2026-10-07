import { afterEach, describe, expect, it, vi } from "vitest";
import { render, renderHook, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { createMemoryHistory } from "@tanstack/react-router";
import type { ReactNode } from "react";
import { meQueryKey } from "@/api/auth";
import { fetchPdf, fileNameOf } from "@/api/pdf";
import { LocalizedRouter } from "@/features/shell/LocalizedRouter";
import { createQueryClient } from "@/lib/session";
import { buildRouter, sendToLogin } from "@/routes";
import { renderAt, signedIn, stubApi, type Answer } from "@/test/app";
import { axeViolations } from "@/test/axe";
import { aPage, aSpace } from "@/test/spaces";
import { PRINT_READY_ATTRIBUTE, loadEveryPicture, pendingIn, usePrintReady } from "./printReady";

afterEach(() => {
  vi.unstubAllGlobals();
  vi.restoreAllMocks();
  document.documentElement.removeAttribute(PRINT_READY_ATTRIBUTE);
  document.documentElement.removeAttribute("data-theme");
});

const pageId = "0195f000-0000-7000-8000-0000000000a1";
const pdfBytes = "%PDF-1.7\n%a test\n";

function guide() {
  return aPage({
    id: pageId,
    title: "Guide",
    home: false,
    version: 4,
    updatedAt: "2026-10-01T08:00:00Z",
    can: { edit: false, delete: false, restrict: false, comment: true, archive: false, add: false, grantEdit: false },
    body: {
      type: "doc",
      content: [
        { type: "heading", attrs: { level: 1 }, content: [{ type: "text", text: "Setting up" }] },
        { type: "paragraph", content: [{ type: "text", text: "Install it first." }] },
      ],
    },
  });
}

/** Answers the PDF route with a file, or with the envelope given. */
function pdfAnswer(refusal?: Answer) {
  return async () =>
    refusal
      ? new Response(JSON.stringify(refusal.body), { status: refusal.status, headers: { "Content-Type": "application/json" } })
      : new Response(pdfBytes, {
          status: 200,
          headers: { "Content-Type": "application/pdf", "Content-Disposition": 'attachment; filename="DOCS-guide-2026-10-07.pdf"' },
        });
}

/** Stubs the page the reader opens, and the PDF route with each answer in turn. */
function stubReading(pdf: (() => Promise<Response>)[]) {
  const space = aSpace();
  const sent = stubApi({
    "GET /spaces": { status: 200, body: { spaces: [space] } },
    "GET /spaces/DOCS": { status: 200, body: { space } },
    "GET /spaces/DOCS/pages": { status: 200, body: { pages: [] } },
    [`GET /pages/${pageId}`]: { status: 200, body: { page: guide(), space } },
    [`GET /pages/${pageId}/attachments`]: { status: 200, body: { attachments: [] } },
    "GET /themes/default": { status: 200, body: { theme: null } },
  });
  const api = globalThis.fetch;
  const printed: string[] = [];
  vi.stubGlobal("fetch", async (input: Request) => {
    const path = new URL(input.url).pathname;
    if (path.endsWith("/pdf")) {
      printed.push(path);
      // The last answer stands for any print after it.
      const next = pdf.length > 1 ? pdf.shift()! : pdf[0]!;
      return next();
    }
    return api(input);
  });
  return { sent, printed };
}

/** jsdom has no object URLs; these stand in. */
function stubObjectUrls() {
  Object.assign(URL, { createObjectURL: () => "blob:pdf", revokeObjectURL: () => {} });
}

/** The print view at a path, outside the shell, as the render service opens it. */
async function renderPrint(path: string) {
  const queryClient = createQueryClient((client) => sendToLogin(router, client));
  queryClient.setQueryData(meQueryKey, signedIn);
  const router = buildRouter(queryClient, createMemoryHistory({ initialEntries: [path] }));
  await router.load();
  render(
    <QueryClientProvider client={queryClient}>
      <LocalizedRouter router={router} />
    </QueryClientProvider>,
  );
}

describe("what is still loading", () => {
  it("is a skeleton, a busy region, a diagram being drawn or a picture not yet in", () => {
    const root = document.createElement("div");
    expect(pendingIn(root)).toBe(false);
    for (const markup of ['<div data-skeleton=""></div>', '<div aria-busy="true"></div>', '<div data-diagram-state="drawing"></div>']) {
      root.innerHTML = markup;
      expect(pendingIn(root)).toBe(true);
    }
    root.innerHTML = '<div aria-busy="false"></div><div data-diagram-state="error"></div>';
    expect(pendingIn(root)).toBe(false);
    root.innerHTML = "<img>";
    Object.defineProperty(root.querySelector("img"), "complete", { value: false });
    expect(pendingIn(root)).toBe(true);
  });

  it("asks for every picture at once, since a print never scrolls to one", () => {
    const root = document.createElement("div");
    root.innerHTML = '<img loading="lazy"><img>';
    loadEveryPicture(root);
    expect([...root.querySelectorAll("img")].map((img) => img.getAttribute("loading"))).toEqual(["eager", null]);
  });

  it("marks the document ready once nothing is pending, and not before the page has loaded", async () => {
    const client = new QueryClient();
    const wrapper = ({ children }: { children: ReactNode }) => <QueryClientProvider client={client}>{children}</QueryClientProvider>;
    const html = document.documentElement;
    const busy = document.createElement("div");
    busy.setAttribute("aria-busy", "true");
    document.body.append(busy);
    const { rerender } = renderHook(({ loaded }) => usePrintReady(loaded), { wrapper, initialProps: { loaded: false } });
    rerender({ loaded: true });
    await new Promise((resolve) => setTimeout(resolve, 400));
    expect(html.hasAttribute(PRINT_READY_ATTRIBUTE)).toBe(false);
    busy.setAttribute("aria-busy", "false");
    await waitFor(() => expect(html.hasAttribute(PRINT_READY_ATTRIBUTE)).toBe(true));
    busy.remove();
  });
});

describe("fetching a PDF", () => {
  it("takes the name the API gives it", () => {
    expect(fileNameOf('attachment; filename="DOCS-guide-2026-10-07.pdf"', "x.pdf")).toBe("DOCS-guide-2026-10-07.pdf");
    expect(fileNameOf("attachment; filename*=UTF-8''%C3%9Cbersicht.pdf", "x.pdf")).toBe("Übersicht.pdf");
    expect(fileNameOf(null, "guide.pdf")).toBe("guide.pdf");
  });

  it("reads a refusal as its sentence, and a lost connection as one of its own", async () => {
    vi.stubGlobal(
      "fetch",
      pdfAnswer({ status: 503, body: { error: { code: "render_busy", message: "Too many PDFs are being made right now. Try again in a minute." } } }),
    );
    await expect(fetchPdf("/api/v1/pages/x/pdf", "x.pdf")).rejects.toMatchObject({
      status: 503,
      code: "render_busy",
      message: expect.stringContaining("Try again"),
    });
    vi.stubGlobal("fetch", async () => {
      throw new TypeError("Failed to fetch");
    });
    await expect(fetchPdf("/api/v1/pages/x/pdf", "x.pdf")).rejects.toMatchObject({
      code: "network",
      message: expect.stringContaining("Check your connection"),
    });
  });
});

describe("exporting a page as PDF", () => {
  async function openExport() {
    await userEvent.click(await screen.findByRole("button", { name: "Page actions" }));
    await userEvent.click(await screen.findByRole("menuitem", { name: "Export as PDF" }));
  }

  it("saves the printed file under the API's name and closes", async () => {
    const { printed } = stubReading([pdfAnswer()]);
    const saved: string[] = [];
    stubObjectUrls();
    vi.spyOn(HTMLAnchorElement.prototype, "click").mockImplementation(function (this: HTMLAnchorElement) {
      saved.push(this.download);
    });
    await renderAt(`/s/DOCS/p/${pageId}/guide`);
    await openExport();
    await waitFor(() => expect(saved).toEqual(["DOCS-guide-2026-10-07.pdf"]));
    await waitFor(() => expect(screen.queryByRole("dialog")).toBeNull());
    expect(new Set(printed)).toEqual(new Set([`/api/v1/pages/${pageId}/pdf`]));
  });

  it("says why a print failed, in words that pass axe, and tries again", async () => {
    const busy = { status: 503, body: { error: { code: "render_busy", message: "Too many PDFs are being made right now. Try again in a minute." } } };
    stubReading([pdfAnswer(busy), pdfAnswer()]);
    stubObjectUrls();
    vi.spyOn(HTMLAnchorElement.prototype, "click").mockImplementation(() => {});
    await renderAt(`/s/DOCS/p/${pageId}/guide`);
    await openExport();
    const dialog = within(await screen.findByRole("dialog"));
    expect(await dialog.findByRole("alert")).toHaveTextContent("Too many PDFs are being made right now.");
    expect(await axeViolations()).toEqual([]);
    await userEvent.click(dialog.getByRole("button", { name: "Retry" }));
    await waitFor(() => expect(screen.queryByRole("dialog")).toBeNull());
  });

  it("is not offered for a page never published", async () => {
    const space = aSpace();
    stubApi({
      "GET /spaces": { status: 200, body: { spaces: [space] } },
      "GET /spaces/DOCS": { status: 200, body: { space } },
      "GET /spaces/DOCS/pages": { status: 200, body: { pages: [] } },
      [`GET /pages/${pageId}`]: { status: 200, body: { page: { ...guide(), unpublished: true, version: 0 }, space } },
      [`GET /pages/${pageId}/attachments`]: { status: 200, body: { attachments: [] } },
    });
    await renderAt(`/s/DOCS/p/${pageId}/guide`);
    await userEvent.click(await screen.findByRole("button", { name: "Page actions" }));
    expect(await screen.findByRole("menuitem", { name: "Export as Markdown" })).toBeVisible();
    expect(screen.queryByRole("menuitem", { name: "Export as PDF" })).toBeNull();
  });
});

describe("the print view", () => {
  it("shows the published page alone, states its header and footer, and says when it is ready", async () => {
    stubReading([]);
    await renderPrint(`/print/p/${pageId}`);
    expect(await screen.findByRole("heading", { level: 1, name: "Guide" })).toBeVisible();
    expect(screen.getByText("Install it first.")).toBeVisible();
    expect(document.querySelector("[data-top-bar]")).toBeNull();
    const meta = document.querySelector<HTMLElement>("[data-print-meta]")!;
    expect(meta.dataset.header).toBe("Demo · Handbook");
    expect(meta.dataset.title).toBe("Guide");
    expect(meta.dataset.footer).toMatch(/^Version 4 of .+ · Printed .+$/);
    expect(meta.dataset.pages).toBe("Page {page} of {pages}");
    expect(document.documentElement.getAttribute("data-theme")).toBe("light");
    await waitFor(() => expect(document.documentElement.hasAttribute(PRINT_READY_ATTRIBUTE)).toBe(true));
    expect(document.title).toBe("Guide");
  });

  it("says why a page cannot be printed, so the service does not wait for it", async () => {
    stubApi({ [`GET /pages/${pageId}`]: { status: 404, body: { error: { code: "not_found", message: "That page was not found." } } } });
    await renderPrint(`/print/p/${pageId}`);
    expect(await screen.findByRole("alert")).toHaveTextContent("That page was not found.");
    expect(document.querySelector("[data-print-failed]")).not.toBeNull();
    expect(document.documentElement.hasAttribute(PRINT_READY_ATTRIBUTE)).toBe(false);
  });

  it("prints the page a public link opens with nothing of its space", async () => {
    const token = "a".repeat(43);
    stubApi({
      [`GET /public/demo/links/${token}`]: {
        status: 200,
        body: {
          site: { slug: "demo", name: "Demo", indexable: false },
          page: { id: pageId, title: "Guide", kind: "page", appearance: guide().appearance, body: guide().body, version: 4, updatedAt: "2026-10-01T08:00:00Z" },
        },
      },
    });
    await renderPrint(`/print/public/demo/link/${token}`);
    expect(await screen.findByRole("heading", { level: 1, name: "Guide" })).toBeVisible();
    expect(document.querySelector<HTMLElement>("[data-print-meta]")!.dataset.header).toBe("Demo");
    await waitFor(() => expect(document.documentElement.hasAttribute(PRINT_READY_ATTRIBUTE)).toBe(true));
  });
});
