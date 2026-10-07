import { afterEach, describe, expect, it, vi } from "vitest";
import { screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import type { Me } from "@/api/auth";
import { arrival, renderAt, signedIn, stubApi } from "@/test/app";
import { axeViolations } from "@/test/axe";
import { aPage, aSpace } from "@/test/spaces";

afterEach(() => vi.unstubAllGlobals());

const space = aSpace();
const gwen = {
  userId: "0195f000-0000-7000-8000-00000000a7e5",
  email: "gwen@contractor.test",
  name: "Gwen Guest",
  role: "commenter",
  permissions: ["view", "addComments"],
  invitedAt: "2026-10-01T08:00:00Z",
};
const member: Me = { ...signedIn, organization: { ...signedIn.organization!, role: "member" } };
const guest: Me = {
  ...signedIn,
  organization: { ...signedIn.organization!, role: "guest", guestSpace: { id: space.id, key: space.key, name: space.name } },
};

async function guestsCard() {
  const heading = await screen.findByRole("heading", { name: "Guests" });
  return heading.closest("[data-space-guests]") as HTMLElement;
}

describe("a space's guests", () => {
  it("are listed for an administrator of the organization, who invites another", async () => {
    let guests = [gwen];
    const hal = { ...gwen, userId: "0195f000-0000-7000-8000-0000000000a1", email: "hal@contractor.test", name: "hal", role: "editor" };
    const sent = stubApi({
      "GET /spaces/DOCS": { status: 200, body: { space } },
      "GET /spaces/DOCS/guests": () => ({ status: 200, body: { guests } }),
      "POST /spaces/DOCS/guests": () => {
        guests = [gwen, hal];
        return { status: 201, body: { guest: hal } };
      },
    });
    await renderAt("/s/DOCS/settings?tab=guests");
    const card = await guestsCard();
    const row = (await within(card).findByText("Gwen Guest")).closest("tr")!;
    expect(row).toHaveTextContent("gwen@contractor.test");
    expect(row).toHaveTextContent("Read and comment");

    await userEvent.type(within(card).getByLabelText("Email address"), "hal@contractor.test");
    await userEvent.selectOptions(within(card).getByLabelText("May"), "editor");
    await userEvent.click(within(card).getByRole("button", { name: "Invite guest" }));
    await waitFor(() => expect(sent.find((r) => r.method === "POST")?.body).toEqual({ email: "hal@contractor.test", role: "editor" }));
    expect(await within(card).findByText("hal@contractor.test may sign in to this space now.")).toBeInTheDocument();
    expect((await within(card).findByText("hal", { selector: "div" })).closest("tr")).toHaveTextContent("Read, comment and edit");
    expect(within(card).getByLabelText("Email address")).toHaveValue("");
    expect(await axeViolations()).toEqual([]);
  });

  it("shows the server's word beside the address", async () => {
    stubApi({
      "GET /spaces/DOCS": { status: 200, body: { space } },
      "GET /spaces/DOCS/guests": { status: 200, body: { guests: [] } },
      "POST /spaces/DOCS/guests": {
        status: 422,
        body: {
          error: {
            code: "validation_failed",
            message: "Some fields need attention.",
            fields: { email: "bob@stator.test is a member of this organization already. Grant them this space under its permissions instead." },
          },
        },
      },
    });
    await renderAt("/s/DOCS/settings?tab=guests");
    const card = await guestsCard();
    expect(await within(card).findByText("This space has no guests.")).toBeInTheDocument();
    await userEvent.type(within(card).getByLabelText("Email address"), "bob@stator.test");
    await userEvent.click(within(card).getByRole("button", { name: "Invite guest" }));
    expect(await within(card).findByText(/is a member of this organization already/)).toBeInTheDocument();
    expect(within(card).getByLabelText("Email address")).toHaveAttribute("aria-invalid", "true");
  });

  it("are removed once the administrator confirms", async () => {
    let guests = [gwen];
    const sent = stubApi({
      "GET /spaces/DOCS": { status: 200, body: { space } },
      "GET /spaces/DOCS/guests": () => ({ status: 200, body: { guests } }),
      [`DELETE /spaces/DOCS/guests/${gwen.userId}`]: () => {
        guests = [];
        return { status: 204 };
      },
    });
    await renderAt("/s/DOCS/settings?tab=guests");
    const card = await guestsCard();
    await userEvent.click(await within(card).findByRole("button", { name: "Remove Gwen Guest" }));
    await userEvent.click(within(card).getByRole("button", { name: /^Remove$/ }));
    await waitFor(() => expect(sent.some((r) => r.method === "DELETE")).toBe(true));
    expect(await within(card).findByText("Gwen Guest was removed.")).toBeInTheDocument();
    expect(await within(card).findByText("This space has no guests.")).toBeInTheDocument();
  });

  it("are no tab for a member", async () => {
    stubApi({ "GET /spaces/DOCS": { status: 200, body: { space } } });
    await renderAt("/s/DOCS/settings?tab=guests", { me: member });
    expect(await screen.findByRole("tab", { name: "Details" })).toHaveAttribute("aria-selected", "true");
    expect(screen.queryByRole("tab", { name: "Guests" })).toBeNull();
  });
});

describe("a guest", () => {
  it("lands in their space, not on the home page", async () => {
    stubApi({
      "GET /spaces/DOCS": { status: 200, body: { space } },
      [`GET /pages/${space.homePageId}`]: { status: 200, body: { page: aPage(), space } },
    });
    const router = await renderAt("/", { me: guest });
    await arrival(router, "/s/DOCS");
    expect(router.state.location.pathname).toBe("/s/DOCS");
  });

  it("is shown no space directory, hub or tokens", async () => {
    stubApi({ "GET /tasks": { status: 200, body: { tasks: [] } } });
    await renderAt("/tasks", { me: guest });
    const rail = document.querySelector<HTMLElement>("[data-rail]")!;
    const sidebar = document.querySelector<HTMLElement>('[data-sidebar="open"]')!;
    for (const place of [rail, sidebar]) {
      expect(within(place).queryByRole("link", { name: /Spaces/ })).toBeNull();
      expect(within(place).getByRole("link", { name: /Search/ })).toBeInTheDocument();
    }
    await userEvent.click(screen.getByRole("button", { name: "Your account" }));
    expect(document.querySelector('[role="menu"] [data-action="profile"]')).not.toBeNull();
    expect(document.querySelector('[role="menu"] [data-action="tokens"]')).toBeNull();
  });
});

describe("the organization's members", () => {
  it("mark a guest with the space they belong to", async () => {
    stubApi({
      "GET /oidc-provider": { status: 200, body: { provider: null, callbackUrl: "http://localhost/cb" } },
      "GET /users/requests": { status: 200, body: { requests: [] } },
      "GET /oidc-provider/group-roles": { status: 200, body: { groupRoles: [] } },
      "GET /users": {
        status: 200,
        body: {
          members: [
            {
              userId: gwen.userId,
              email: gwen.email,
              name: gwen.name,
              role: "guest",
              roleSource: "manual",
              joinedAt: gwen.invitedAt,
              guestSpace: { id: space.id, key: space.key, name: space.name },
            },
          ],
        },
      },
    });
    await renderAt("/settings/sso");
    const row = (await screen.findByText("Gwen Guest")).closest("tr")!;
    expect(row).toHaveAttribute("data-member", gwen.email);
    expect(within(row).getByText("Guest")).toBeInTheDocument();
    expect(within(row).getByRole("link", { name: "Guest of Handbook" })).toHaveAttribute("href", "/s/DOCS/settings?tab=guests");
  });
});
