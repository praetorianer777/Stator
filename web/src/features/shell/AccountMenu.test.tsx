import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import type { Me } from "@/api/auth";
import { renderAt, signedIn, stubApi, unauthorized } from "@/test/app";

beforeEach(() => localStorage.clear());
afterEach(() => vi.unstubAllGlobals());

const member: Me = {
  ...signedIn,
  user: {
    id: "u-bob",
    email: "bob@stator.test",
    name: "Bob Builder",
    avatarUrl: "https://pictures.test/bob.png",
    locale: "",
    timezone: "",
    showInReaders: true,
  },
  organization: { ...signedIn.organization!, role: "member", administers: false },
};

const guest: Me = {
  ...signedIn,
  organization: { ...signedIn.organization!, role: "guest", administers: false, guestSpace: { id: "s-eng", key: "ENG", name: "Engineering" } },
};

async function openMenu() {
  await userEvent.click(screen.getByRole("button", { name: "Your account" }));
  return screen.getByRole("menu", { name: "Your account" });
}

describe("the account menu", () => {
  it("shows who is signed in by picture, and their name, email and role at its head", async () => {
    await renderAt("/", { me: member });
    expect(screen.getByRole("img", { name: "Bob Builder" })).toHaveAttribute("src", "https://pictures.test/bob.png");
    await openMenu();
    const head = document.querySelector<HTMLElement>("[data-account-header]")!;
    expect(head).toHaveTextContent("Bob Builder");
    expect(head).toHaveTextContent("bob@stator.test");
    expect(head.querySelector("[data-account-role]")).toHaveTextContent("Member");
  });

  it("is a round button that opens past the rail's edge, its avatar beside who is signed in", async () => {
    await renderAt("/", { me: member });
    const trigger = screen.getByRole("button", { name: "Your account" });
    expect(trigger).toHaveClass("rounded-full");
    // The menu is anchored on a wrapper as wide as the rail, so it opens beside the rail, not over it.
    expect(trigger.parentElement).toHaveClass("w-full");
    expect(document.querySelector("[data-rail-foot]")).toHaveClass("w-full");
    await openMenu();
    const head = document.querySelector<HTMLElement>("[data-account-header]")!;
    expect(within(head).getByRole("img", { name: "Bob Builder" })).toHaveAttribute("src", "https://pictures.test/bob.png");
  });

  it("sets Admin and Sign out apart from the person's own pages", async () => {
    await renderAt("/");
    const menu = await openMenu();
    const separators = within(menu).getAllByRole("separator");
    expect(separators).toHaveLength(2);
    expect(separators[0]!.nextElementSibling).toHaveTextContent("Admin");
    expect(separators[1]!.nextElementSibling).toHaveTextContent("Sign out");
  });

  it("draws initials for somebody without a picture", async () => {
    await renderAt("/");
    const trigger = screen.getByRole("button", { name: "Your account" });
    expect(within(trigger).getByText("AL")).toBeInTheDocument();
  });

  it("holds an administrator's own pages, one Admin entry and Sign out, and no page of the installation's", async () => {
    await renderAt("/");
    const menu = await openMenu();
    expect(
      within(menu)
        .getAllByRole("menuitem")
        .map((item) => item.textContent),
    ).toEqual(["Your profile", "Notification settings", "Watching", "Themes", "Access tokens", "Admin", "Sign out"]);
  });

  it("opens the Admin page from Admin", async () => {
    const router = await renderAt("/");
    await userEvent.click(within(await openMenu()).getByRole("menuitem", { name: "Admin" }));
    await waitFor(() => expect(router.state.location.pathname).toBe("/admin"));
  });

  it("gives a member their own pages without Admin", async () => {
    stubApi({ "GET /access/me": { status: 200, body: { can: { use: true, createSpace: false, administer: false } } } });
    await renderAt("/", { me: member });
    const menu = await openMenu();
    await waitFor(() => expect(within(menu).queryByRole("menuitem", { name: "Admin" })).toBeNull());
    expect(
      within(menu)
        .getAllByRole("menuitem")
        .map((item) => item.textContent),
    ).toEqual(["Your profile", "Notification settings", "Watching", "Themes", "Access tokens", "Sign out"]);
  });

  it("gives a guest no Access tokens and no Admin", async () => {
    stubApi({ "GET /access/me": { status: 200, body: { can: { use: true, createSpace: false, administer: false } } } });
    await renderAt("/", { me: guest });
    const menu = await openMenu();
    await waitFor(() => expect(within(menu).queryByRole("menuitem", { name: "Admin" })).toBeNull());
    expect(
      within(menu)
        .getAllByRole("menuitem")
        .map((item) => item.textContent),
    ).toEqual(["Your profile", "Notification settings", "Watching", "Themes", "Sign out"]);
  });

  it("signs out and lands on the sign-in page", async () => {
    const sent = stubApi({ "POST /auth/logout": { status: 204 }, "GET /auth/me": unauthorized });
    const router = await renderAt("/spaces");
    await userEvent.click(within(await openMenu()).getByRole("menuitem", { name: "Sign out" }));
    await waitFor(() => expect(router.state.location.pathname).toBe("/login"));
    expect(sent.some((r) => r.method === "POST" && r.path === "/auth/logout")).toBe(true);
    expect(await screen.findByRole("heading", { name: "Sign in" })).toBeInTheDocument();
  });
});

const armature = { product: "armature", url: "https://armature.example.test" };
const exciter = { product: "exciter", url: "https://exciter.example.test" };

describe("the tools to switch to", () => {
  it("links to each connected tool's home, as a plain link", async () => {
    stubApi({ "GET /connected-tools": { status: 200, body: { tools: [armature, exciter] } } });
    await renderAt("/");
    const group = await within(await openMenu()).findByRole("group", { name: "Switch to" });
    const links = within(group).getAllByRole("menuitem");
    expect(links.map((l) => l.textContent)).toEqual(["Armature", "Exciter"]);
    expect(within(group).getByRole("menuitem", { name: "Armature" })).toHaveAttribute("href", "https://armature.example.test");
    expect(within(group).getByRole("menuitem", { name: "Exciter" })).toHaveAttribute("href", "https://exciter.example.test");
    expect(links.every((l) => l.tagName === "A" && !l.hasAttribute("target"))).toBe(true);
    // Each tool's own mark, in the one format the suite's tools share.
    expect(links.map((l) => l.querySelector("img")?.getAttribute("src"))).toEqual(["/tools/armature.webp", "/tools/exciter.webp"]);
  });

  it("lists only the one connected", async () => {
    stubApi({ "GET /connected-tools": { status: 200, body: { tools: [exciter] } } });
    await renderAt("/", { me: member });
    const group = await within(await openMenu()).findByRole("group", { name: "Switch to" });
    expect(
      within(group)
        .getAllByRole("menuitem")
        .map((l) => l.textContent),
    ).toEqual(["Exciter"]);
  });

  it("leaves the group out when none is connected", async () => {
    const sent = stubApi({ "GET /connected-tools": { status: 200, body: { tools: [] } } });
    await renderAt("/", { me: member });
    await waitFor(() => expect(sent.some((r) => r.path === "/connected-tools")).toBe(true));
    const menu = await openMenu();
    expect(within(menu).queryByRole("group", { name: "Switch to" })).toBeNull();
    expect(within(menu).queryByRole("menuitem", { name: "Armature" })).toBeNull();
  });

  it("shows them to a guest too, since the other tool decides what they see", async () => {
    stubApi({ "GET /connected-tools": { status: 200, body: { tools: [armature] } } });
    await renderAt("/", { me: guest });
    const group = await within(await openMenu()).findByRole("group", { name: "Switch to" });
    expect(within(group).getByRole("menuitem", { name: "Armature" })).toHaveAttribute("href", "https://armature.example.test");
  });
});

describe("a session that ends while the page is open", () => {
  it("sends the reader to sign in, and back to the page afterwards", async () => {
    stubApi({ "GET /oidc-provider": unauthorized, "GET /auth/me": unauthorized });
    const router = await renderAt("/admin/access/sso");
    await waitFor(() => expect(router.state.location.pathname).toBe("/login"));
    expect(router.state.location.search).toEqual({ next: "/admin/access/sso" });
  });
});
