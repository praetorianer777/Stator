import { afterEach, describe, expect, it, vi } from "vitest";
import { cleanup, render, screen, waitFor, within } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import type { ReactNode } from "react";
import type { ArmatureLink } from "@/api/armature";
import { stubApi, type Answer } from "@/test/app";
import { axeViolations } from "@/test/axe";
import { aPage } from "@/test/spaces";
import { ArmatureLinks } from "./ArmatureLinks";

afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
});

const BASE = "https://armature.example.com";
const PAGE = "0195f000-0000-7000-8000-0000000000aa";

const account: Answer = {
  status: 200,
  body: { account: { configured: true, baseUrl: BASE, connected: true, status: "ok", user: null, checkedAt: null } },
};
const links = (...rows: ArmatureLink[]): Answer => ({ status: 200, body: { links: rows } });
const refusal =
  "Armature did not let Bob link pages to this issue. Somebody who may edit the issue in Armature can change the page. The next change to the page tries again.";

function wrap(children: ReactNode) {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <QueryClientProvider client={client}>
      <main>
        <h1>Page</h1>
        {children}
      </main>
    </QueryClientProvider>,
  );
}

describe("the page's links in Armature", () => {
  it("lists each issue the page names with whether it is linked, and says why one is not", async () => {
    stubApi({
      "GET /armature/account": account,
      [`GET /pages/${PAGE}/armature-links`]: links(
        { key: "CP-1", state: "synced", error: null, syncedAt: "2026-10-01T10:00:00Z" },
        { key: "CP-2", state: "failed", error: refusal, syncedAt: null },
      ),
    });
    wrap(<ArmatureLinks page={aPage({ id: PAGE })} />);
    const section = await screen.findByRole("region", { name: "Linked in Armature" });
    const [linked, refused] = within(section).getAllByRole("listitem") as [HTMLElement, HTMLElement];
    expect([linked, refused].map((row) => row.getAttribute("data-state"))).toEqual(["synced", "failed"]);
    expect(linked).toHaveTextContent("Linked");
    await waitFor(() => expect(within(linked).getByRole("link", { name: "CP-1" })).toHaveAttribute("href", `${BASE}/issues/CP-1`));
    expect(refused).toHaveTextContent("Not linked");
    expect(refused).toHaveTextContent(refusal);
    expect(linked).not.toHaveTextContent("Armature did not");
    expect(await axeViolations()).toEqual([]);
  });

  it("asks again while a link waits for the worker", async () => {
    let asked = 0;
    stubApi({
      "GET /armature/account": account,
      [`GET /pages/${PAGE}/armature-links`]: () => {
        asked += 1;
        return links({ key: "CP-1", state: asked > 1 ? "synced" : "pending", error: null, syncedAt: null });
      },
    });
    wrap(<ArmatureLinks page={aPage({ id: PAGE })} />);
    const row = await screen.findByText("Waiting for Armature");
    expect(row).toBeInTheDocument();
    await waitFor(() => expect(screen.getByText("Linked")).toBeInTheDocument(), { timeout: 5000 });
  });

  it("shows nothing for a page that names no issue, and asks nothing for an unpublished one", async () => {
    const sent = stubApi({ "GET /armature/account": account, [`GET /pages/${PAGE}/armature-links`]: links() });
    const { container } = wrap(<ArmatureLinks page={aPage({ id: PAGE })} />);
    await waitFor(() => expect(sent.some((each) => each.path.endsWith("/armature-links"))).toBe(true));
    expect(container.querySelector("[data-armature-links]")).toBeNull();
    cleanup();

    const unsent = stubApi({ "GET /armature/account": account });
    wrap(<ArmatureLinks page={aPage({ id: PAGE, unpublished: true })} />);
    await waitFor(() => expect(unsent.some((each) => each.path === "/armature/account")).toBe(true));
    expect(unsent.some((each) => each.path.endsWith("/armature-links"))).toBe(false);
  });
});
