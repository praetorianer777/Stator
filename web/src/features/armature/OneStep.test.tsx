import { afterEach, describe, expect, it, vi } from "vitest";
import { screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import type { ArmatureAccount, ArmatureConnection } from "@/api/armature";
import { renderAt, stubApi } from "@/test/app";
import { axeViolations } from "@/test/axe";

afterEach(() => {
  vi.unstubAllGlobals();
  vi.restoreAllMocks();
});

const BASE = "https://armature.example.com";
const RECEIVER = "https://stator.example.com/api/v1/armature/webhook";
const GUARD =
  "Armature may not reach stator.example.com, which is inside its own network. Add stator.example.com to network.outboundAllow (ARMATURE_OUTBOUND_ALLOW) and try again.";

function connection(hook: Partial<ArmatureConnection["partnerWebhook"]> = {}): ArmatureConnection {
  return {
    baseUrl: BASE,
    installationId: null,
    webhookSecretSet: Boolean(hook.connected),
    webhookUrl: RECEIVER,
    webhookTopics: ["issue.created", "issue.updated", "issue.transitioned", "comment.added"],
    lastDelivery: null,
    connected: 1,
    updatedAt: "2026-10-01T08:00:00Z",
    partner: {
      status: "ok",
      meta: true,
      version: "1.5.0",
      capabilities: ["issue-embed@1", "oauth.partner-tokens@1", "webhooks.partner@1", "webhooks@1"],
      missing: [],
    },
    app: {
      offered: true,
      status: "active",
      clientId: "0199e1c2-0000-7000-8000-0000000000aa",
      registeredBy: "stator",
      refusal: null,
      redirectUri: "https://stator.example.com/api/v1/armature/oauth/callback",
      scopes: ["read", "issue.write"],
    },
    partnerWebhook: { offered: true, connected: false, url: RECEIVER, refusal: null, manageable: false, ...hook },
  };
}

const admin: ArmatureAccount = {
  configured: true,
  baseUrl: BASE,
  connected: true,
  via: "app",
  status: "ok",
  user: { id: "0195f000-0000-7000-8000-0000000000b1", name: "Ada", email: "ada@acme.armature.test" },
  checkedAt: "2026-10-01T09:00:00Z",
  missing: [],
  app: { offered: true, pending: false, refusal: null, scopes: ["read", "issue.write", "webhooks.partner"], signInRefusal: null },
};

const notConnected: ArmatureAccount = { ...admin, connected: false, via: null, status: "not_connected", user: null, checkedAt: null };

describe("connecting Armature in one step", () => {
  it("offers Connect instead of the pasted secret, which stays behind a disclosure", async () => {
    stubApi({
      "GET /armature/connection": { status: 200, body: { connection: connection() } },
      "GET /armature/account": { status: 200, body: { account: notConnected } },
    });
    const user = userEvent.setup();
    await renderAt("/settings/armature");
    const section = (await screen.findByRole("heading", { name: "Connect in one step" })).closest("section")!;
    expect(within(section).getByText(/no secret is copied by hand/)).toBeInTheDocument();
    expect(within(section).getByRole("link", { name: "Connect" })).toHaveAttribute("href", "/api/v1/armature/connection/webhook/connect");
    expect(screen.queryByLabelText("Webhook secret")).toBeNull();
    expect(screen.queryByRole("button", { name: "Copy address" })).toBeNull();
    expect(screen.getByLabelText("Webhook address")).toHaveValue(RECEIVER);
    // What members do next, with the administrator's own Connect.
    expect(screen.getByRole("heading", { name: "What members do next" })).toBeInTheDocument();
    expect(await screen.findByRole("link", { name: "Connect your own account" })).toBeInTheDocument();
    expect(await axeViolations()).toEqual([]);

    await user.click(screen.getByRole("button", { name: "Paste a webhook secret instead" }));
    expect(screen.getByLabelText("Webhook secret")).toBeInTheDocument();
    expect(screen.queryByRole("heading", { name: "Connect in one step" })).toBeNull();
    expect(screen.getByRole("button", { name: "Copy address" })).toBeInTheDocument();
  });

  it("says the webhook is made once back from Armature, and tests it and gives it a new secret", async () => {
    let current = connection({ connected: true, manageable: true });
    const sent = stubApi({
      "GET /armature/connection": () => ({ status: 200, body: { connection: current } }),
      "GET /armature/account": { status: 200, body: { account: admin } },
      "POST /armature/connection/webhook/test": { status: 200, body: { test: { status: 204, error: null, deliveredAt: "2026-10-01T09:01:00Z" } } },
      "POST /armature/connection/webhook/rotate-secret": () => {
        current = connection({ connected: true, manageable: true });
        return { status: 200, body: { connection: current } };
      },
    });
    vi.spyOn(window, "confirm").mockReturnValue(true);
    const user = userEvent.setup();
    await renderAt("/settings/armature?armature=connected");
    expect(await screen.findByText("Armature is connected: Stator made its webhook there, and your own account is connected.")).toBeInTheDocument();
    expect(screen.getByText(`Stator made its own webhook in Armature, which posts every change to ${RECEIVER}.`)).toBeInTheDocument();
    expect(screen.queryByRole("link", { name: "Connect your own account" })).toBeNull();
    expect(screen.queryByLabelText("Webhook secret")).toBeNull();

    await user.click(screen.getByRole("button", { name: "Test" }));
    expect(await screen.findByText("Armature reached Stator, which answered 204.")).toBeInTheDocument();
    await user.click(screen.getByRole("button", { name: "New secret" }));
    expect(await screen.findByText("The webhook has a new secret, which Stator stored.")).toBeInTheDocument();
    expect(sent.filter((r) => r.method === "POST").map((r) => r.path)).toEqual([
      "/armature/connection/webhook/test",
      "/armature/connection/webhook/rotate-secret",
    ]);
    expect(await axeViolations()).toEqual([]);
  });

  it("shows Armature's own sentence when its outbound guard refuses Stator's address", async () => {
    stubApi({
      "GET /armature/connection": { status: 200, body: { connection: connection({ refusal: GUARD }) } },
      "GET /armature/account": { status: 200, body: { account: admin } },
    });
    await renderAt("/settings/armature?armature=webhook_refused");
    expect(await screen.findByText("Your account is connected, but Armature would not make the webhook. What it said is below.")).toBeInTheDocument();
    expect(screen.getByText("Armature cannot reach Stator, so it would not make the webhook. Armature said:")).toBeInTheDocument();
    expect(document.querySelector("[data-armature-webhook-refusal]")).toHaveTextContent(GUARD);
    expect(screen.getByRole("link", { name: "Connect again" })).toHaveAttribute("href", "/api/v1/armature/connection/webhook/connect");
    expect(await axeViolations()).toEqual([]);
  });

  it("asks for an Armature administrator when the grant may not manage the webhook", async () => {
    const said =
      "An Armature administrator must connect Stator again before its webhook can be changed. Choose Connect under Settings, Armature, and allow it in Armature as an Armature administrator.";
    stubApi({
      "GET /armature/connection": { status: 200, body: { connection: connection({ connected: true, manageable: true }) } },
      "GET /armature/account": { status: 200, body: { account: admin } },
      "POST /armature/connection/webhook/test": { status: 409, body: { error: { code: "armature_webhook_admin", message: said } } },
    });
    const user = userEvent.setup();
    await renderAt("/settings/armature?armature=webhook_admin");
    expect(await screen.findByText(/An Armature administrator must connect again: sign in to Armature as one/)).toBeInTheDocument();
    await user.click(screen.getByRole("button", { name: "Test" }));
    expect(await screen.findByText(said)).toBeInTheDocument();
  });

  it("tells another administrator to connect themselves before they test or renew it", async () => {
    stubApi({
      "GET /armature/connection": { status: 200, body: { connection: connection({ connected: true, manageable: false }) } },
      "GET /armature/account": { status: 200, body: { account: notConnected } },
    });
    await renderAt("/settings/armature");
    expect(await screen.findByText(/connect again as an Armature administrator yourself/)).toBeInTheDocument();
    expect(screen.getByRole("link", { name: "Connect again" })).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Test" })).toBeNull();
  });

  it("keeps the pasted secret for an Armature without webhooks.partner@1", async () => {
    stubApi({
      "GET /armature/connection": { status: 200, body: { connection: connection({ offered: false }) } },
      "GET /armature/account": { status: 200, body: { account: admin } },
    });
    await renderAt("/settings/armature");
    expect(await screen.findByLabelText("Webhook secret")).toBeInTheDocument();
    expect(screen.queryByRole("heading", { name: "Connect in one step" })).toBeNull();
    expect(screen.queryByRole("button", { name: "Paste a webhook secret instead" })).toBeNull();
  });
});
