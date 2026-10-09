import { afterEach, describe, expect, it, vi } from "vitest";
import { screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { renderAt, stubApi } from "@/test/app";
import { axeViolations } from "@/test/axe";

afterEach(() => vi.unstubAllGlobals());

const owner = {
  userId: "0195f000-0000-7000-8000-000000000001",
  email: "owner@stator.test",
  name: "Olive Owner",
  role: "owner",
  roleSource: "manual",
  joinedAt: "2026-09-01T08:00:00Z",
  guestSpace: null,
};
const provider = { provider: null, callbackUrl: "http://localhost:5173/api/v1/auth/oidc/callback" };

const settings = (extra: Parameters<typeof stubApi>[0] = {}) =>
  stubApi({
    "GET /oidc-provider": { status: 200, body: provider },
    "GET /users/requests": { status: 200, body: { requests: [] } },
    "GET /users": { status: 200, body: { members: [owner] } },
    "GET /oidc-provider/group-roles": { status: 200, body: { groupRoles: [] } },
    ...extra,
  });

async function membersCard() {
  const heading = await screen.findByRole("heading", { name: "Members" });
  return heading.closest("[data-members]") as HTMLElement;
}

describe("adding a person with a password", () => {
  it("sends what was typed and shows a made password once, with a way to copy it", async () => {
    const sent = settings({
      "POST /users": {
        status: 201,
        body: {
          member: {
            userId: "0195f000-0000-7000-8000-000000000002",
            email: "ada@stator.test",
            name: "Ada",
            role: "member",
            newAccount: true,
            password: "Kx7mQ2pRt9WvZa4Nb6Hc",
          },
        },
      },
    });
    await renderAt("/settings/sso");
    const card = await membersCard();
    await userEvent.click(within(card).getByRole("button", { name: "Add person" }));
    await userEvent.type(within(card).getByLabelText("Email address"), " ada@stator.test ");
    await userEvent.type(within(card).getByLabelText("Name"), "Ada");
    await userEvent.click(within(card).getByRole("button", { name: "Add" }));

    await waitFor(() => expect(sent.some((r) => r.method === "POST" && r.path === "/users")).toBe(true));
    expect(sent.find((r) => r.method === "POST" && r.path === "/users")?.body).toEqual({ email: "ada@stator.test", name: "Ada", role: "member", password: "" });
    const added = await within(card).findByText("Ada was added and can sign in now.");
    expect(added).toBeInTheDocument();
    expect(card.querySelector("[data-new-password]")).toHaveTextContent("Kx7mQ2pRt9WvZa4Nb6Hc");
    expect(within(card).getByText(/shown only now/)).toBeInTheDocument();
    // The form is closed and cleared once the person is in.
    expect(within(card).queryByLabelText("Email address")).toBeNull();
  });

  it("does not show a password that was chosen, and says so for a person who already had an account", async () => {
    settings({
      "POST /users": {
        status: 201,
        body: { member: { userId: "0195f000-0000-7000-8000-000000000003", email: "bob@stator.test", name: "Bob", role: "admin", newAccount: false } },
      },
    });
    await renderAt("/settings/sso");
    const card = await membersCard();
    await userEvent.click(within(card).getByRole("button", { name: "Add person" }));
    await userEvent.type(within(card).getByLabelText("Email address"), "bob@stator.test");
    await userEvent.selectOptions(within(card).getByLabelText("Role"), "admin");
    await userEvent.type(within(card).getByLabelText("Password"), "a password of my own");
    await userEvent.click(within(card).getByRole("button", { name: "Add" }));
    expect(await within(card).findByText(/already had an account/)).toBeInTheDocument();
    expect(card.querySelector("[data-new-password]")).toBeNull();
  });

  it("puts a refusal beside the field it is about", async () => {
    settings({
      "POST /users": {
        status: 422,
        body: {
          error: {
            code: "validation_failed",
            message: "Check the form.",
            fields: { password: "Password must be at least 12 characters. Leave it empty to have one made." },
          },
        },
      },
    });
    await renderAt("/settings/sso");
    const card = await membersCard();
    await userEvent.click(within(card).getByRole("button", { name: "Add person" }));
    await userEvent.type(within(card).getByLabelText("Email address"), "cy@stator.test");
    await userEvent.type(within(card).getByLabelText("Password"), "short");
    await userEvent.click(within(card).getByRole("button", { name: "Add" }));
    expect(await within(card).findByText(/at least 12 characters\. Leave it empty/)).toBeInTheDocument();
    expect(within(card).getByLabelText("Password")).toHaveAttribute("aria-invalid", "true");
  });

  it("passes axe with the form open", async () => {
    settings();
    await renderAt("/settings/sso");
    const card = await membersCard();
    await userEvent.click(within(card).getByRole("button", { name: "Add person" }));
    expect(await axeViolations()).toEqual([]);
  });
});
