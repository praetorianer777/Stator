import { afterEach, describe, expect, it, vi } from "vitest";
import { screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import type { ArmatureAccount, ArmatureConnection } from "@/api/armature";
import { arrival, renderAt, stubApi } from "@/test/app";
import { axeViolations } from "@/test/axe";

afterEach(() => {
  vi.unstubAllGlobals();
  vi.restoreAllMocks();
});

const BASE = "https://armature.example.com";
const TOKEN = "armature_pat_acme_ada";
const SECRET = "armature_whs_abcdefghijklmnopqrstuvwxyz0123456789ABCDEFG";
const ARMATURE_CAPABILITIES = [
  "calendar@1",
  "issue-embed@1",
  "plan@1",
  "remote-link-relations@1",
  "remote-links@1",
  "reports@1",
  "theme-export@1",
  "webhooks@1",
];
const REFUSED = "Armature did not accept this token. Make a new one under Tokens in Armature and paste it here.";

function connection(over: Partial<ArmatureConnection> = {}): ArmatureConnection {
  return {
    baseUrl: BASE,
    orgSlug: "acme",
    armatureOrgId: null,
    webhookSecretSet: false,
    webhookUrl: "http://stator.test/api/v1/armature/webhook/demo",
    webhookTopics: ["issue.created", "issue.updated", "issue.transitioned", "comment.added"],
    lastDelivery: null,
    connected: 0,
    updatedAt: "2026-10-01T08:00:00Z",
    partner: { status: "ok", meta: true, version: "1.3.0", capabilities: ARMATURE_CAPABILITIES, missing: [] },
    ...over,
  };
}

function account(over: Partial<ArmatureAccount> = {}): ArmatureAccount {
  return { configured: true, baseUrl: BASE, connected: false, status: "not_connected", user: null, checkedAt: null, missing: [], ...over };
}

const connectedAccount = account({
  connected: true,
  status: "ok",
  user: { id: "0195f000-0000-7000-8000-0000000000b1", name: "Ada", email: "ada@acme.armature.test" },
  checkedAt: "2026-10-01T09:00:00Z",
});

describe("connecting Armature as an administrator", () => {
  it("connects an instance with its webhook secret, which never comes back", async () => {
    let current: ArmatureConnection | null = null;
    const sent = stubApi({
      "GET /armature/connection": () => ({ status: 200, body: { connection: current } }),
      "PUT /armature/connection": () => {
        current = connection({ webhookSecretSet: true });
        return { status: 200, body: { connection: current } };
      },
      "GET /armature/account": { status: 200, body: { account: account() } },
    });
    const user = userEvent.setup();
    await renderAt("/settings/armature");
    expect(await screen.findByRole("heading", { level: 1, name: "Armature" })).toBeInTheDocument();
    expect(document.querySelector("[data-copy-status]")).toBeNull();

    await user.type(screen.getByLabelText("Armature address"), `${BASE}/`);
    await user.type(screen.getByLabelText("Armature organization"), "acme");
    await user.type(screen.getByLabelText("Webhook secret"), SECRET);
    await user.click(screen.getByRole("button", { name: "Connect Armature" }));

    expect(await screen.findByText("Saved.")).toBeInTheDocument();
    expect(sent.find((r) => r.method === "PUT")?.body).toEqual({ baseUrl: `${BASE}/`, orgSlug: "acme", webhookSecret: SECRET });
    expect(screen.getByLabelText("Webhook secret")).toHaveValue("");
    expect(screen.getByLabelText("Webhook secret")).toHaveAttribute("placeholder", "Stored. Leave blank to keep it.");
    expect(screen.getByLabelText("Webhook address")).toHaveValue("http://stator.test/api/v1/armature/webhook/demo");
    expect(within(screen.getByRole("list", { name: "Topics" })).getAllByRole("listitem")).toHaveLength(4);
    expect(screen.getByRole("heading", { name: "What members do next" })).toBeInTheDocument();
    expect(screen.getByText(/Each member now connects their own Armature account/)).toBeInTheDocument();
    expect(await screen.findByRole("link", { name: "Connect your own account" })).toHaveAttribute(
      "href",
      "/settings/profile?return=%2Fsettings%2Farmature#armature",
    );
    expect(await axeViolations()).toEqual([]);
  });

  it("says when the webhook has not reached Stator yet", async () => {
    stubApi({ "GET /armature/connection": { status: 200, body: { connection: connection() } } });
    await renderAt("/settings/armature");
    expect(await screen.findByText("Stator has not taken a delivery from Armature yet.")).toBeInTheDocument();
  });

  it("says in which envelope version the last delivery came, and that its time was signed", async () => {
    const lastDelivery = { receivedAt: "2026-10-15T09:30:00Z", topic: "issue.updated", version: 1, timestamped: true };
    stubApi({ "GET /armature/connection": { status: 200, body: { connection: connection({ lastDelivery }) } } });
    await renderAt("/settings/armature");
    const said = await screen.findByText(/Stator last took a delivery, issue\.updated/);
    expect(said).toHaveTextContent(/in envelope version 1, signed with the time it was sent\.$/);
  });

  it("asks for an Armature that signs the time when the last delivery came without one", async () => {
    const lastDelivery = { receivedAt: "2026-10-15T09:30:00Z", topic: "ping", version: null, timestamped: false };
    stubApi({ "GET /armature/connection": { status: 200, body: { connection: connection({ lastDelivery }) } } });
    await renderAt("/settings/armature");
    expect(await screen.findByText(/without an envelope version, so from an older Armature, without a signed time\. Update Armature/)).toBeInTheDocument();
  });

  it("keeps the stored secret unless told to forget it, and shows a refusal by its field", async () => {
    const sent = stubApi({
      "GET /armature/connection": { status: 200, body: { connection: connection({ webhookSecretSet: true, connected: 3 }) } },
      "PUT /armature/connection": {
        status: 422,
        body: {
          error: {
            code: "validation_failed",
            message: "Some fields need attention.",
            fields: { baseUrl: "Armature did not answer at this address. Check the address, and that Armature is running, then save again." },
          },
        },
      },
    });
    const user = userEvent.setup();
    await renderAt("/settings/armature");
    expect(await screen.findByText(/3 members have connected their accounts/)).toBeInTheDocument();
    await user.click(screen.getByRole("button", { name: "Save" }));
    expect(await screen.findByText(/Armature did not answer at this address/)).toBeInTheDocument();
    expect(screen.getByLabelText("Armature address")).toHaveAttribute("aria-invalid", "true");
    expect(sent.filter((r) => r.method === "PUT")[0]?.body).toEqual({ baseUrl: BASE, orgSlug: "acme" });

    await user.click(screen.getByLabelText("Forget the stored secret"));
    await user.click(screen.getByRole("button", { name: "Save" }));
    await waitFor(() => expect(sent.filter((r) => r.method === "PUT")).toHaveLength(2));
    expect(sent.filter((r) => r.method === "PUT")[1]?.body).toEqual({ baseUrl: BASE, orgSlug: "acme", webhookSecret: "" });
  });

  it("disconnects after asking", async () => {
    let current: ArmatureConnection | null = connection();
    stubApi({
      "GET /armature/connection": () => ({ status: 200, body: { connection: current } }),
      "DELETE /armature/connection": () => {
        current = null;
        return { status: 204 };
      },
      "GET /armature/account": { status: 200, body: { account: account({ configured: false, baseUrl: null, status: "not_configured" }) } },
    });
    const confirm = vi.spyOn(window, "confirm").mockReturnValue(true);
    const user = userEvent.setup();
    await renderAt("/settings/armature");
    await user.click(await screen.findByRole("button", { name: "Disconnect Armature" }));
    expect(confirm).toHaveBeenCalled();
    expect(await screen.findByText("Armature is disconnected.")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Connect Armature" })).toBeInTheDocument();
  });

  it("says which features Stator turns off for an Armature that lacks their capability", async () => {
    stubApi({
      "GET /armature/connection": {
        status: 200,
        body: {
          connection: connection({
            partner: {
              status: "ok",
              meta: true,
              version: "1.2.0",
              capabilities: ARMATURE_CAPABILITIES.filter((c) => c !== "reports@1"),
              missing: [{ feature: "charts", capability: "reports@1" }],
            },
          }),
        },
      },
    });
    await renderAt("/settings/armature");
    expect(await screen.findByText("Armature 1.2.0 answers at this address.")).toBeInTheDocument();
    expect(document.querySelector("[data-armature-capabilities]")).toHaveAttribute("data-armature-capabilities", "missing");
    expect(document.querySelector('[data-missing="charts"]')).toHaveTextContent(
      "Stator turns off charts, because Armature does not offer reports@1. Update Armature to a release that does.",
    );
    expect(await axeViolations()).toEqual([]);
  });

  it("says an Armature from before /meta is too old to say what it offers", async () => {
    stubApi({
      "GET /armature/connection": {
        status: 200,
        body: {
          connection: connection({
            partner: { status: "ok", meta: false, version: null, capabilities: [], missing: [{ feature: "issues", capability: "issue-embed@1" }] },
          }),
        },
      },
    });
    await renderAt("/settings/armature");
    expect(await screen.findByText(/This Armature is too old to say what it offers/)).toBeInTheDocument();
    expect(document.querySelector('[data-missing="issues"]')).toHaveTextContent(/issue chips, blocks and lists/);
  });

  it("tells a member the page is for administrators", async () => {
    stubApi({ "GET /armature/connection": { status: 403, body: { error: { code: "forbidden", message: "Only an administrator." } } } });
    await renderAt("/settings/armature");
    expect(await screen.findByText("Only an administrator can connect Armature.")).toBeInTheDocument();
  });
});

describe("connecting one's own Armature account", () => {
  it("says to ask an administrator while the organization has no connection", async () => {
    stubApi({ "GET /armature/account": { status: 200, body: { account: account({ configured: false, baseUrl: null, status: "not_configured" }) } } });
    await renderAt("/settings/profile");
    expect(await screen.findByText(/Ask an administrator to connect it under Settings, Armature/)).toBeInTheDocument();
    expect(screen.queryByLabelText("Armature token")).toBeNull();
  });

  it("pastes a token, shows whom it acts as and never the token", async () => {
    let current = account();
    const sent = stubApi({
      "GET /armature/account": () => ({ status: 200, body: { account: current } }),
      "PUT /armature/account/token": () => {
        current = connectedAccount;
        return { status: 200, body: { account: current } };
      },
    });
    const user = userEvent.setup();
    await renderAt("/settings/profile");
    expect(await screen.findByRole("heading", { level: 1, name: "Your profile" })).toBeInTheDocument();
    expect(screen.getByRole("link", { name: /Open Armature's tokens/ })).toHaveAttribute("href", `${BASE}/settings/tokens`);
    expect(screen.getByLabelText("Armature token")).not.toHaveFocus();
    await user.type(await screen.findByLabelText("Armature token"), TOKEN);
    await user.click(screen.getByRole("button", { name: "Connect" }));

    expect(await screen.findByText("Connected as Ada (ada@acme.armature.test).")).toBeInTheDocument();
    expect(sent.find((r) => r.method === "PUT")?.body).toEqual({ token: TOKEN });
    expect(screen.queryByLabelText("Armature token")).toBeNull();
    expect(document.body).not.toHaveTextContent(TOKEN);
    expect(await axeViolations()).toEqual([]);
  });

  it("shows Armature's refusal of a token as the sentence by the field", async () => {
    stubApi({
      "GET /armature/account": { status: 200, body: { account: account() } },
      "PUT /armature/account/token": {
        status: 422,
        body: { error: { code: "validation_failed", message: "Some fields need attention.", fields: { token: REFUSED } } },
      },
    });
    const user = userEvent.setup();
    await renderAt("/settings/profile");
    await user.type(await screen.findByLabelText("Armature token"), "armature_pat_revoked");
    await user.click(screen.getByRole("button", { name: "Connect" }));
    expect(await screen.findByText(REFUSED)).toBeInTheDocument();
    expect(screen.getByLabelText("Armature token")).toHaveAttribute("aria-invalid", "true");
  });

  it("says Armature is down in words when it does not answer", async () => {
    stubApi({
      "GET /armature/account": { status: 200, body: { account: account() } },
      "PUT /armature/account/token": {
        status: 502,
        body: {
          error: { code: "armature_unreachable", message: "Armature did not answer. Try again in a moment, and tell an administrator if it keeps happening." },
        },
      },
    });
    const user = userEvent.setup();
    await renderAt("/settings/profile");
    await user.type(await screen.findByLabelText("Armature token"), TOKEN);
    await user.click(screen.getByRole("button", { name: "Connect" }));
    expect(await screen.findByText(/Armature did not answer. Try again in a moment/)).toBeInTheDocument();
  });

  it("checks a token, offers a new one when Armature rejects it, and disconnects", async () => {
    let current = connectedAccount;
    stubApi({
      "GET /armature/account": () => ({ status: 200, body: { account: current } }),
      "POST /armature/account/check": () => {
        current = { ...connectedAccount, status: "rejected" };
        return { status: 200, body: { account: current } };
      },
      "DELETE /armature/account/token": () => {
        current = account();
        return { status: 204 };
      },
    });
    vi.spyOn(window, "confirm").mockReturnValue(true);
    const user = userEvent.setup();
    await renderAt("/settings/profile");
    await user.click(await screen.findByRole("button", { name: "Check again" }));
    expect(await screen.findByText(/Armature no longer accepts your token/)).toBeInTheDocument();
    expect(screen.getByLabelText("Paste a new token")).toBeInTheDocument();

    await user.click(screen.getByRole("button", { name: "Disconnect" }));
    expect(await screen.findByText("Your Armature account is disconnected.")).toBeInTheDocument();
    expect(await screen.findByLabelText("Armature token")).toBeInTheDocument();
  });

  it("is reached from the account menu", async () => {
    stubApi({ "GET /armature/account": { status: 200, body: { account: account() } } });
    const user = userEvent.setup();
    const router = await renderAt("/");
    await user.click(screen.getByRole("button", { name: "Your account" }));
    await user.click(within(screen.getByRole("menu", { name: "Your account" })).getByRole("menuitem", { name: "Your profile" }));
    await waitFor(() => expect(router.state.location.pathname).toBe("/settings/profile"));
    expect(await screen.findByRole("heading", { name: "Armature" })).toBeInTheDocument();
  });
});

describe("coming to the token form from a connect hint", () => {
  function connecting() {
    let current = account();
    return stubApi({
      "GET /armature/account": () => ({ status: 200, body: { account: current } }),
      "PUT /armature/account/token": () => {
        current = connectedAccount;
        return { status: 200, body: { account: current } };
      },
    });
  }

  it("focuses the token field, and once connected goes back to the page with a notice", async () => {
    connecting();
    const user = userEvent.setup();
    const router = await renderAt("/settings/profile?return=%2Fsettings%2Fthemes%3Fx%3D1#armature");
    const field = await screen.findByLabelText("Armature token");
    await waitFor(() => expect(field).toHaveFocus());
    expect(screen.getByText("Once Armature accepts the token, you go back to the page you came from.")).toBeInTheDocument();
    await user.keyboard(TOKEN);
    await user.click(screen.getByRole("button", { name: "Connect" }));

    await arrival(router, "/settings/themes");
    expect(router.state.location.href).toBe("/settings/themes?x=1");
    expect(await screen.findByText("Your Armature account is connected, and here is the page you came from.")).toBeInTheDocument();
    expect(await axeViolations()).toEqual([]);

    await router.navigate({ to: "/" });
    await arrival(router, "/");
    await waitFor(() => expect(document.querySelector("[data-armature-returned]")).toBeNull());
  });

  it("takes the notice down when asked", async () => {
    connecting();
    const user = userEvent.setup();
    const router = await renderAt("/settings/profile?return=%2Fsettings%2Fthemes#armature");
    await user.type(await screen.findByLabelText("Armature token"), TOKEN);
    await user.click(screen.getByRole("button", { name: "Connect" }));
    await arrival(router, "/settings/themes");
    const notice = await screen.findByText(/here is the page you came from/);
    await user.click(within(notice.closest("[data-armature-returned]") as HTMLElement).getByRole("button", { name: "Close" }));
    expect(document.querySelector("[data-armature-returned]")).toBeNull();
  });

  it("stays on the profile for a way back that leads off this site", async () => {
    connecting();
    const user = userEvent.setup();
    const router = await renderAt(`/settings/profile?return=${encodeURIComponent("//evil.example/pages")}#armature`);
    await waitFor(() => expect(screen.getByLabelText("Armature token")).toHaveFocus());
    expect(screen.queryByText(/you go back to the page you came from/)).toBeNull();
    await user.keyboard(TOKEN);
    await user.click(screen.getByRole("button", { name: "Connect" }));
    expect(await screen.findByText("Connected as Ada (ada@acme.armature.test).")).toBeInTheDocument();
    expect(router.state.location.pathname).toBe("/settings/profile");
    expect(document.querySelector("[data-armature-returned]")).toBeNull();
  });
});
