import { afterEach, describe, expect, it, vi } from "vitest";
import { cleanup, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import type { ReactNode } from "react";
import type { RemoteLink } from "@/api/remoteLinks";
import { applyLanguage } from "@/i18n";
import { stubApi, type Answer } from "@/test/app";
import { axeViolations } from "@/test/axe";
import { aPage } from "@/test/spaces";
import { PageRemoteLinks } from "./PageRemoteLinks";

afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
});

const PAGE = "0195f000-0000-7000-8000-0000000000aa";
const LINKS = `GET /pages/${PAGE}/remote-links`;

function aLink(over: Partial<RemoteLink>): RemoteLink {
  return {
    via: null,
    id: "0195f000-0000-7000-8000-00000000f001",
    url: "https://exciter.example/r/1",
    title: "REQ-1 Sign in with a passkey (specifies)",
    source: "Exciter",
    iconUrl: null,
    createdBy: "0195f000-0000-7000-8000-00000000a1ce",
    createdByName: "Alice",
    createdAt: "2026-10-15T09:00:00Z",
    updatedAt: "2026-10-15T09:00:00Z",
    ...over,
  };
}

const exciterOne = aLink({});
const exciterTwo = aLink({
  id: "0195f000-0000-7000-8000-00000000f002",
  url: "https://exciter.example/r/2",
  title: "REQ-2 Recover an account (describes)",
  createdBy: null,
  createdByName: "",
});
const armature = aLink({
  id: "0195f000-0000-7000-8000-00000000f003",
  url: "https://armature.example/issues/CP-1",
  title: "CP-1 Passkeys",
  source: "Armature",
  iconUrl: "https://armature.example/favicon.png",
});
const listed = (...rows: RemoteLink[]): Answer => ({ status: 200, body: { remoteLinks: rows } });

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

describe("the links other tools put on a page", () => {
  it("lists them under their source with the icon, the title as a link and who put each", async () => {
    stubApi({ [LINKS]: listed(armature, exciterOne, exciterTwo) });
    wrap(<PageRemoteLinks page={aPage({ id: PAGE, can: { ...aPage().can, edit: false } })} />);
    const section = await screen.findByRole("region", { name: "Linked from other tools" });
    expect(
      within(section)
        .getAllByRole("heading", { level: 3 })
        .map((h) => h.textContent),
    ).toEqual(["Armature (1)", "Exciter (2)"]);
    const exciter = within(section).getByRole("list", { name: "Exciter" });
    const [one, two] = within(exciter).getAllByRole("listitem") as [HTMLElement, HTMLElement];
    const link = within(one).getByRole("link", { name: exciterOne.title });
    expect(link).toHaveAttribute("href", exciterOne.url);
    expect(link).toHaveAttribute("target", "_blank");
    expect(link).toHaveAttribute("rel", "noopener noreferrer");
    expect(one).toHaveTextContent("Put by Alice");
    expect(two).toHaveTextContent("Put by somebody no longer here");
    expect(one.querySelector("[data-remote-link-icon]")).toBeNull();
    const icon = within(section).getByRole("list", { name: "Armature" }).querySelector("img[data-remote-link-icon]");
    expect(icon).toHaveAttribute("src", armature.iconUrl);
    expect(within(section).queryByRole("button")).toBeNull();
    expect(await axeViolations()).toEqual([]);
  });

  it("shows the source's initial when the icon does not load", async () => {
    stubApi({ [LINKS]: listed(armature) });
    wrap(<PageRemoteLinks page={aPage({ id: PAGE })} />);
    const section = await screen.findByRole("region", { name: "Linked from other tools" });
    fireEvent.error(section.querySelector("img[data-remote-link-icon]")!);
    await waitFor(() => expect(section.querySelector("img")).toBeNull());
    expect(within(section).getByRole("listitem")).toHaveTextContent(/^ACP-1/);
  });

  it("lets whoever may edit the page take a link off, and says so", async () => {
    let rows = [exciterOne, exciterTwo];
    const sent = stubApi({
      [LINKS]: () => listed(...rows),
      [`DELETE /pages/${PAGE}/remote-links/${exciterOne.id}`]: () => {
        rows = [exciterTwo];
        return { status: 204 };
      },
    });
    wrap(<PageRemoteLinks page={aPage({ id: PAGE })} />);
    fireEvent.click(await screen.findByRole("button", { name: `Remove the link ${exciterOne.title}` }));
    await waitFor(() => expect(screen.queryByRole("link", { name: exciterOne.title })).toBeNull());
    expect(sent.some((each) => each.method === "DELETE")).toBe(true);
    expect(screen.getByRole("status")).toHaveTextContent(`Removed the link ${exciterOne.title}.`);
    expect(screen.getByRole("link", { name: exciterTwo.title })).toBeInTheDocument();
  });

  it("says why a link could not be taken off", async () => {
    stubApi({
      [LINKS]: listed(exciterOne),
      [`DELETE /pages/${PAGE}/remote-links/${exciterOne.id}`]: {
        status: 403,
        body: { error: { code: "forbidden", message: "You may not edit this page. Ask somebody who may edit it." } },
      },
    });
    wrap(<PageRemoteLinks page={aPage({ id: PAGE })} />);
    fireEvent.click(await screen.findByRole("button", { name: `Remove the link ${exciterOne.title}` }));
    expect(await screen.findByText("You may not edit this page. Ask somebody who may edit it.")).toBeInTheDocument();
    expect(screen.getByRole("link", { name: exciterOne.title })).toBeInTheDocument();
  });

  it("shows nothing for a page without links, even to an editor", async () => {
    const sent = stubApi({ [LINKS]: listed() });
    const { container } = wrap(<PageRemoteLinks page={aPage({ id: PAGE })} />);
    await waitFor(() => expect(sent.some((each) => each.path.endsWith("/remote-links"))).toBe(true));
    expect(container.querySelector("[data-remote-links]")).toBeNull();
  });

  it("speaks German", async () => {
    applyLanguage("de");
    try {
      stubApi({ [LINKS]: listed(exciterTwo) });
      wrap(<PageRemoteLinks page={aPage({ id: PAGE })} />);
      const section = await screen.findByRole("region", { name: "Aus anderen Werkzeugen verknüpft" });
      expect(section).toHaveTextContent("Gesetzt von jemandem, der nicht mehr hier ist");
      expect(within(section).getByRole("button", { name: `Link ${exciterTwo.title} entfernen` })).toBeInTheDocument();
    } finally {
      applyLanguage("");
    }
  });
});
