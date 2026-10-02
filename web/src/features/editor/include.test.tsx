import { afterEach, describe, expect, it, vi } from "vitest";
import { cleanup, render, screen, waitFor } from "@testing-library/react";
import { Editor as TiptapEditor } from "@tiptap/core";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import type { ReactNode } from "react";
import type { Included } from "@/api/included";
import { stubApi, type Answer } from "@/test/app";
import { axeViolations } from "@/test/axe";
import { DocPageContext } from "./BlockViews";
import { DocView } from "./DocView";
import { editorExtensions } from "./extensions";
import type { Doc, DocNode } from "./schema";

afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
});

const HERE = "0199a1b2-c3d4-7e5f-8a9b-0c1d2e3f4a90";
const SUPPORT = "0199a1b2-c3d4-7e5f-8a9b-0c1d2e3f4a91";
const SECRET = "0199a1b2-c3d4-7e5f-8a9b-0c1d2e3f4a92";
const HOURS = "0199a1b2-c3d4-7e5f-8a9b-0c1d2e3f4a71";

const line = (text: string): DocNode => ({ type: "paragraph", content: [{ type: "text", text }] });
const include = (pageId: string, excerptId: string | null = null): DocNode => ({ type: "include", attrs: { pageId, excerptId } });
const included = (id: string, title: string, body: Doc, excerpt: Included["excerpt"] = null): Answer => ({
  status: 200,
  body: { included: { page: { id, title, spaceKey: "DOCS" }, excerpt, body } },
});

function shown(children: ReactNode) {
  return render(
    <QueryClientProvider client={new QueryClient({ defaultOptions: { queries: { retry: false } } })}>
      <main>
        <h1>Here</h1>
        <DocPageContext value={{ id: HERE, spaceKey: "DOCS" }}>{children}</DocPageContext>
      </main>
    </QueryClientProvider>,
  );
}

describe("an include in the read-only view", () => {
  it("shows the page's words, or one excerpt's, framed and named with a link to where they come from", async () => {
    const sent = stubApi({
      [`GET /pages/${SUPPORT}/included`]: (request) =>
        new URL(request.url).searchParams.get("excerpt") === HOURS
          ? included(SUPPORT, "Support", { type: "doc", content: [line("Nine to five")] }, { id: HOURS, name: "Hours" })
          : included(SUPPORT, "Support", { type: "doc", content: [line("All of support")] }),
    });
    shown(<DocView doc={{ type: "doc", content: [include(SUPPORT), include(SUPPORT, HOURS)] }} />);
    const whole = await screen.findByRole("region", { name: "Included from Support" });
    expect(whole).toHaveTextContent("All of support");
    const part = await screen.findByRole("region", { name: "Included from Support: Hours" });
    expect(part).toHaveTextContent("Nine to five");
    expect(screen.getByRole("link", { name: "Included from Support: Hours" })).toHaveAttribute("href", `/s/DOCS/p/${SUPPORT}/support`);
    // The page being read is the start of the chain each include is asked with.
    expect(sent.filter((r) => r.path.endsWith("/included"))).toHaveLength(2);
    expect(await axeViolations()).toEqual([]);
  });

  it("tells a reader who may not read the page so, and nothing more", async () => {
    stubApi({ [`GET /pages/${SECRET}/included`]: { status: 404, body: { error: { code: "not_found", message: "That page was not found." } } } });
    shown(<DocView doc={{ type: "doc", content: [include(SECRET)] }} />);
    expect(await screen.findByRole("note")).toHaveTextContent("This included content is not available to you");
  });

  it("asks with the chain of pages it sits in, and shows the server's word when it leads back", async () => {
    const asked: string[] = [];
    stubApi({
      [`GET /pages/${SUPPORT}/included`]: (request) => {
        asked.push(`${SUPPORT}<${new URL(request.url).searchParams.get("via")}`);
        return included(SUPPORT, "Support", { type: "doc", content: [line("Support says"), include(HERE)] });
      },
      [`GET /pages/${HERE}/included`]: (request) => {
        asked.push(`${HERE}<${new URL(request.url).searchParams.get("via")}`);
        return {
          status: 409,
          body: { error: { code: "include_cycle", message: "This include leads back to a page that includes it, so it is shown only once." } },
        };
      },
    });
    shown(<DocView doc={{ type: "doc", content: [include(SUPPORT)] }} />);
    await waitFor(() => expect(screen.getByRole("note")).toHaveTextContent("leads back to a page that includes it"));
    expect(asked).toEqual([`${SUPPORT}<${HERE}`, `${HERE}<${HERE},${SUPPORT}`]);
    expect(screen.getByRole("region", { name: "Included from Support" })).toHaveTextContent("Support says");
  });
});

describe("the include block in the editor", () => {
  it("does not include the page being edited", () => {
    const e = new TiptapEditor({ element: document.createElement("div"), extensions: editorExtensions({ pageId: HERE }) });
    expect(e.commands.insertInclude({ pageId: HERE, excerptId: null })).toBe(false);
    expect(e.commands.insertInclude({ pageId: SUPPORT, excerptId: HOURS })).toBe(true);
    expect((e.getJSON().content ?? []).find((n) => n.type === "include")?.attrs).toEqual({ pageId: SUPPORT, excerptId: HOURS });
    e.destroy();
  });
});
