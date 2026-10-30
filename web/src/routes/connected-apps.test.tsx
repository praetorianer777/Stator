import { afterEach, describe, expect, it, vi } from "vitest";
import { screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { answerConfirm, renderAt, stubApi } from "@/test/app";
import { axeViolations } from "@/test/axe";

afterEach(() => vi.unstubAllGlobals());

const armature = {
  id: "0195f000-0000-7000-8000-0000000000a1",
  name: "Armature",
  scopes: ["read", "write"],
  kind: "partner",
  status: "allowed",
  keycloakClientId: "armature",
  clientUri: "",
  redirectUris: [],
  confidential: true,
  grants: 3,
  createdAt: "2026-09-01T08:00:00Z",
  updatedAt: "2026-09-01T08:00:00Z",
};
const assistant = {
  ...armature,
  id: "0195f000-0000-7000-8000-0000000000a2",
  name: "Desk assistant",
  kind: "registered",
  status: "pending",
  keycloakClientId: "",
  clientUri: "https://assistant.test",
  grants: 0,
  confidential: false,
};
const token = {
  id: "0195f000-0000-7000-8000-0000000000t1",
  name: "Deploy",
  scopes: [],
  spaces: [],
  allSpaces: true,
  lastUsedAt: null,
  expiresAt: null,
  createdAt: "2026-09-01T08:00:00Z",
  clientId: null,
  owner: { id: "u-bob", email: "bob@stator.test", name: "Bob Builder" },
};

describe("the Connected apps page", () => {
  it("lists the apps and every token, adds a partner app and shows its secret once", async () => {
    const sent = stubApi({
      "GET /oauth/clients": { status: 200, body: { clients: [armature, assistant] } },
      "GET /site/tokens": { status: 200, body: { tokens: [token] } },
      "POST /oauth/clients": {
        status: 201,
        body: {
          client: {
            ...armature,
            id: "0195f000-0000-7000-8000-0000000000a3",
            name: "Exciter",
            keycloakClientId: "exciter",
            grants: 0,
            secret: "stator_ocs_once",
          },
        },
      },
    });
    await renderAt("/admin/connected-apps");
    const row = (await screen.findByText("Desk assistant")).closest("tr")!;
    expect(within(row).getByText("Waiting to be allowed")).toBeInTheDocument();
    expect(document.querySelector('[data-connected-app="Armature"]')).toHaveTextContent("3 people");
    expect(await screen.findByText("Deploy")).toBeInTheDocument();
    expect(await axeViolations()).toEqual([]);

    await userEvent.click(screen.getByRole("button", { name: "Add app" }));
    const form = screen.getByRole("form", { name: "Add a partner app" });
    await userEvent.type(within(form).getByLabelText("Name"), "Exciter");
    await userEvent.type(within(form).getByLabelText("Identity provider client"), "exciter");
    await userEvent.click(within(form).getByRole("button", { name: "Add app" }));
    await waitFor(() => expect(sent.some((r) => r.method === "POST")).toBe(true));
    expect(sent.find((r) => r.method === "POST")?.body).toEqual({ name: "Exciter", keycloakClientId: "exciter", scopes: ["read"] });
    expect(await screen.findByText("stator_ocs_once")).toBeInTheDocument();
  });

  it("revokes anybody's token after asking", async () => {
    const sent = stubApi({
      "GET /oauth/clients": { status: 200, body: { clients: [] } },
      "GET /site/tokens": { status: 200, body: { tokens: [token] } },
      [`DELETE /site/tokens/${token.id}`]: { status: 204 },
    });
    await renderAt("/admin/connected-apps");
    await userEvent.click(await screen.findByRole("button", { name: "Revoke Deploy of Bob Builder" }));
    await answerConfirm(true);
    expect(await screen.findByText("Deploy was revoked.")).toBeInTheDocument();
    expect(sent.some((r) => r.method === "DELETE" && r.path === `/site/tokens/${token.id}`)).toBe(true);
  });
});
