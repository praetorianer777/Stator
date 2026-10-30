import { afterEach, describe, expect, it, vi } from "vitest";
import { screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import type { Me } from "@/api/auth";
import { renderAt, adminNavigation, signedIn, stubApi } from "@/test/app";
import { axeViolations } from "@/test/axe";

afterEach(() => vi.unstubAllGlobals());

const carol = { userId: "0195f000-0000-7000-8000-00000000c0c0", email: "carol@stator.test", name: "Carol New", requestedAt: "2026-09-29T08:00:00Z" };
const dave = { userId: "0195f000-0000-7000-8000-00000000da7e", email: "dave@stator.test", name: "", requestedAt: "2026-09-29T08:05:00Z" };
const provider = { provider: null, callbackUrl: "http://localhost:5173/api/v1/auth/oidc/callback" };

describe("the people waiting to be let in", () => {
  it("are listed under Users, and let in as members with one click", async () => {
    let waiting = [carol, dave];
    const sent = stubApi({
      "GET /users/requests": () => ({ status: 200, body: { requests: waiting } }),
      "GET /oidc-provider": { status: 200, body: provider },
      "GET /users": { status: 200, body: { members: [] } },
      [`POST /users/requests/${carol.userId}/admit`]: () => {
        waiting = [dave];
        return { status: 200, body: { admitted: { userId: "u-carol", role: "member" } } };
      },
    });
    await renderAt("/admin/users");
    const list = await screen.findByRole("heading", { name: "Waiting to be let in" });
    const card = list.closest("[data-join-requests]") as HTMLElement;
    const row = within(card).getByText("Carol New").closest("li") as HTMLElement;
    expect(row).toHaveTextContent("carol@stator.test, asked");
    // Somebody the provider named no one is shown by address.
    expect(within(card).getAllByText("dave@stator.test").length).toBeGreaterThan(0);

    await userEvent.click(within(row).getByRole("button", { name: "Let in as member" }));
    await waitFor(() => expect(sent.some((r) => r.method === "POST")).toBe(true));
    expect(sent.find((r) => r.method === "POST")?.body).toEqual({ role: "member" });
    expect(await screen.findByText("Carol New may sign in now.")).toBeInTheDocument();
    await waitFor(() => expect(screen.queryByText("Carol New")).toBeNull());
  });

  it("are turned away, which asks the server to forget the request", async () => {
    let waiting = [carol];
    const sent = stubApi({
      "GET /users/requests": () => ({ status: 200, body: { requests: waiting } }),
      "GET /oidc-provider": { status: 200, body: provider },
      "GET /users": { status: 200, body: { members: [] } },
      [`DELETE /users/requests/${carol.userId}`]: () => {
        waiting = [];
        return { status: 204 };
      },
    });
    await renderAt("/admin/users");
    await userEvent.click(await screen.findByRole("button", { name: "Turn away" }));
    await waitFor(() => expect(sent.some((r) => r.method === "DELETE" && r.path === `/users/requests/${carol.userId}`)).toBe(true));
    expect(await screen.findByText("Carol New was turned away.")).toBeInTheDocument();
  });

  it("take no room when there are none", async () => {
    stubApi({ "GET /users/requests": { status: 200, body: { requests: [] } }, "GET /users": { status: 200, body: { members: [] } } });
    await renderAt("/admin/users");
    await screen.findByText("Nobody is a member yet.");
    expect(document.querySelector("[data-join-requests]")).toBeNull();
  });

  it("show a refusal as it came", async () => {
    stubApi({
      "GET /users/requests": { status: 200, body: { requests: [carol] } },
      "GET /oidc-provider": { status: 200, body: provider },
      "GET /users": { status: 200, body: { members: [] } },
      [`POST /users/requests/${carol.userId}/admit`]: {
        status: 404,
        body: { error: { code: "not_found", message: "That person is not waiting to be let in. Reload the list; somebody may have answered already." } },
      },
    });
    await renderAt("/admin/users");
    await userEvent.click(await screen.findByRole("button", { name: "Let in as member" }));
    expect(await screen.findByRole("alert")).toHaveTextContent("That person is not waiting to be let in.");
  });

  it("pass axe", async () => {
    stubApi({ "GET /users/requests": { status: 200, body: { requests: [carol, dave] } }, "GET /users": { status: 200, body: { members: [] } } });
    await renderAt("/admin/users");
    await screen.findByText("Carol New");
    expect(await axeViolations()).toEqual([]);
  });
});

describe("the count of people waiting", () => {
  it("shows beside Users in an administrator's Admin navigation", async () => {
    stubApi({ "GET /users/requests": { status: 200, body: { requests: [carol, dave] } }, "GET /users": { status: 200, body: { members: [] } } });
    await renderAt("/admin/users");
    const item = await within(await adminNavigation()).findByRole("link", { name: /^Users\W+2 waiting$/ });
    expect(within(item).getByText("2")).toBeInTheDocument();
  });

  it("is not asked for on behalf of a member", async () => {
    const member: Me = { ...signedIn, organization: { ...signedIn.organization!, role: "member", administers: false } };
    const sent = stubApi({ "GET /users/requests": { status: 200, body: { requests: [carol] } } });
    await renderAt("/settings", { me: member });
    await screen.findByRole("navigation", { name: "Settings" });
    expect(screen.queryByRole("link", { name: /^Admin(\W+\d+ waiting)?$/ })).toBeNull();
    expect(sent.some((r) => r.path === "/users/requests")).toBe(false);
  });
});
