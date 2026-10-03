import { afterEach, describe, expect, it, vi } from "vitest";
import { cleanup, render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import type { ReactNode } from "react";
import type { LinkPreview } from "@/api/linkPreview";
import { allowlist, problems } from "@/test/allowlist";
import { stubApi } from "@/test/app";
import { axeViolations } from "@/test/axe";
import { DocView } from "./DocView";
import { Editor } from "./Editor";
import { shortAddress, webAddress } from "./LinkCardViews";
import type { Doc, DocNode } from "./schema";

afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
});

function withQueries(children: ReactNode) {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return <QueryClientProvider client={client}>{children}</QueryClientProvider>;
}

function find(doc: DocNode | undefined, type: string): DocNode[] {
  if (!doc) return [];
  const out: DocNode[] = doc.type === type ? [doc] : [];
  for (const child of doc.content ?? []) out.push(...find(child, type));
  return out;
}

const POST = "https://example.test/blog/post";
const VIDEO = "https://youtu.be/dQw4w9WgXcQ";
const previews: Record<string, LinkPreview> = {
  [POST]: { url: POST, title: "A post worth reading", description: "What the post is about.", siteName: "Example Blog", fetched: true, embed: null },
  [VIDEO]: {
    url: VIDEO,
    title: "A video",
    description: "",
    siteName: "YouTube",
    fetched: true,
    embed: { provider: "YouTube", kind: "video", src: "https://www.youtube-nocookie.com/embed/dQw4w9WgXcQ" },
  },
};

function stubPreviews() {
  return stubApi({
    "GET /link-preview": (request) => {
      const url = new URL(request.url).searchParams.get("url") ?? "";
      return previews[url] ? { status: 200, body: { preview: previews[url] } } : { status: 422, body: { error: { code: "validation", message: "Bad" } } };
    },
  });
}

describe("addresses", () => {
  it("are web pages' only, and read as host and path", () => {
    expect(webAddress(` ${POST} `)).toBe(POST);
    for (const bad of ["example.test", "/spaces/x", "javascript:alert(1)", "mailto:a@example.test", "https://a b.test", "https://user:pw@example.test", 7]) {
      expect(webAddress(bad), String(bad)).toBeNull();
    }
    expect(shortAddress("https://www.example.test/a/b?c")).toBe("example.test/a/b");
    expect(shortAddress("https://example.test/")).toBe("example.test");
    expect(allowlist.nodes.linkCard?.attrs?.view?.enum).toEqual(["card", "embed"]);
  });
});

describe("the read-only view", () => {
  it("shows a card with what the page says, leading to it in a new tab", async () => {
    stubPreviews();
    const { container } = render(
      withQueries(
        <main>
          <h1>Reading list</h1>
          <DocView doc={{ type: "doc", content: [{ type: "linkCard", attrs: { url: POST, view: "card" } }] }} />
        </main>,
      ),
    );
    const link = await screen.findByRole("link", { name: /A post worth reading/ });
    expect(link).toHaveAttribute("href", POST);
    expect(link).toHaveAttribute("target", "_blank");
    expect(link.getAttribute("rel")).toContain("noopener");
    expect(link).toHaveTextContent("Example Blog");
    expect(link).toHaveTextContent("What the post is about.");
    expect(container.querySelector("iframe")).toBeNull();
    expect(await axeViolations()).toEqual([]);
  });

  it("embeds the site's player in a sandbox when asked, and a card when the site has none", async () => {
    stubPreviews();
    const { container } = render(
      withQueries(
        <DocView
          doc={{
            type: "doc",
            content: [
              { type: "linkCard", attrs: { url: VIDEO, view: "embed" } },
              { type: "linkCard", attrs: { url: POST, view: "embed" } },
            ],
          }}
        />,
      ),
    );
    const frame = await screen.findByTitle("YouTube: A video");
    expect(frame).toHaveAttribute("src", "https://www.youtube-nocookie.com/embed/dQw4w9WgXcQ");
    expect(frame.getAttribute("sandbox")).not.toContain("allow-top-navigation");
    expect(container.querySelectorAll("iframe")).toHaveLength(1);
    await screen.findByRole("link", { name: /A post worth reading/ });
  });

  it("names the site by its address when the page could not be read", async () => {
    stubPreviews();
    render(withQueries(<DocView doc={{ type: "doc", content: [{ type: "linkCard", attrs: { url: "https://unknown.test/x", view: "card" } }] }} />));
    await waitFor(() => expect(screen.getByRole("link")).toHaveTextContent("unknown.test/x"));
  });
});

describe("the link card block", () => {
  function setup(value: Doc | null = null) {
    const user = userEvent.setup();
    const onChange = vi.fn();
    render(
      withQueries(
        <main>
          <Editor id="page-body" value={value} onChange={onChange} />
        </main>,
      ),
    );
    const box = document.getElementById("page-body")!;
    return { user, box, last: () => onChange.mock.calls.at(-1)?.[0] as Doc | undefined };
  }

  it("comes from an address pasted alone on a line, and becomes the player when the site has one", async () => {
    stubPreviews();
    const { user, box, last } = setup();
    await user.click(box);
    await user.paste(VIDEO);
    await waitFor(() => expect(find(last(), "linkCard")[0]?.attrs).toEqual({ url: VIDEO, view: "embed" }));
    expect(problems(last()!)).toEqual([]);
    await waitFor(() => expect(box.querySelector('[data-link-card-view="embed"]')).toHaveAttribute("aria-pressed", "true"));
  });

  it("is not made from an address pasted into words, which stays a link", async () => {
    stubPreviews();
    const { user, box, last } = setup();
    await user.click(box);
    await user.type(box, "Read ");
    await user.paste(POST);
    await waitFor(() =>
      expect(
        find(last(), "text")
          .map((n) => n.text)
          .join(""),
      ).toBe(`Read ${POST}`),
    );
    expect(find(last(), "linkCard")).toHaveLength(0);
  });

  it("changes view from its toolbar, inline as a link titled as its page", async () => {
    stubPreviews();
    const { user, box, last } = setup({ type: "doc", content: [{ type: "linkCard", attrs: { url: POST, view: "card" } }] });
    const views = await within(box).findByRole("group", { name: "Show the link as" });
    await waitFor(() => expect(box.querySelector("[data-link-card-title]")).toHaveTextContent("A post worth reading"));
    expect(within(views).getByRole("button", { name: "Embed" })).toBeDisabled();
    expect(within(views).getByRole("button", { name: "Card" })).toHaveAttribute("aria-pressed", "true");
    await user.click(within(views).getByRole("button", { name: "Inline" }));
    await waitFor(() => expect(find(last(), "linkCard")).toHaveLength(0));
    const text = find(last(), "text")[0];
    expect(text?.text).toBe("A post worth reading");
    expect(text?.marks?.[0]).toMatchObject({ type: "link", attrs: { href: POST } });
    expect(problems(last()!)).toEqual([]);
  });

  it("is asked for from the slash menu, refusing what is not a web address", async () => {
    stubPreviews();
    const { user, box, last } = setup();
    await user.click(box);
    await user.type(box, "/link");
    await screen.findByRole("listbox", { name: "Insert a block" });
    await user.keyboard("{Enter}");
    const dialog = await screen.findByRole("dialog", { name: "Link preview" });
    const address = within(dialog).getByLabelText("Address");
    await user.type(address, "example.test");
    await user.click(within(dialog).getByRole("button", { name: "Insert" }));
    expect(within(dialog).getByText(/starting with https:\/\/ or http:\/\//)).toBeInTheDocument();
    await user.clear(address);
    await user.type(address, POST);
    await user.keyboard("{Enter}");
    await waitFor(() => expect(screen.queryByRole("dialog")).toBeNull());
    expect(find(last(), "linkCard")[0]?.attrs).toEqual({ url: POST, view: "card" });
  });
});
