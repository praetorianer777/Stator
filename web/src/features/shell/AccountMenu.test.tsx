import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import type { Me } from "@/api/auth";
import { renderAt, signedIn, stubApi, unauthorized } from "@/test/app";

beforeEach(() => localStorage.clear());
afterEach(() => vi.unstubAllGlobals());

const member: Me = {
  ...signedIn,
  user: { id: "u-bob", email: "bob@stator.test", name: "Bob Builder", avatarUrl: "https://pictures.test/bob.png", locale: "", showInReaders: true },
  organization: { ...signedIn.organization!, role: "member" },
};

async function openMenu() {
  await userEvent.click(screen.getByRole("button", { name: "Your account" }));
  return screen.getByRole("menu", { name: "Your account" });
}

describe("the account menu", () => {
  it("shows who is signed in, by name and picture", async () => {
    await renderAt("/", { me: member });
    expect(document.querySelector("[data-account-name]")).toHaveTextContent("Bob Builder");
    expect(screen.getByRole("img", { name: "Bob Builder" })).toHaveAttribute("src", "https://pictures.test/bob.png");
  });

  it("draws initials for somebody without a picture", async () => {
    await renderAt("/");
    const trigger = screen.getByRole("button", { name: "Your account" });
    expect(within(trigger).getByText("AL")).toBeInTheDocument();
    expect(document.querySelector("[data-account-name]")).toHaveTextContent("Ada Lovelace");
  });

  it("offers single sign-on settings to administrators only", async () => {
    await renderAt("/");
    expect(within(await openMenu()).getByRole("menuitem", { name: "Single sign-on" })).toBeInTheDocument();
  });

  it("keeps them from members", async () => {
    await renderAt("/", { me: member });
    expect(within(await openMenu()).queryByRole("menuitem", { name: "Single sign-on" })).toBeNull();
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

describe("a session that ends while the page is open", () => {
  it("sends the reader to sign in, and back to the page afterwards", async () => {
    stubApi({ "GET /oidc-provider": unauthorized, "GET /auth/me": unauthorized });
    const router = await renderAt("/settings/sso");
    await waitFor(() => expect(router.state.location.pathname).toBe("/login"));
    expect(router.state.location.search).toEqual({ next: "/settings/sso" });
  });
});
