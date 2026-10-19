import { afterEach, describe, expect, it, vi } from "vitest";
import { screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import type { ArmatureAccount, ArmatureConnection } from "@/api/armature";
import { arrival, renderAt, stubApi } from "@/test/app";
import { axeViolations } from "@/test/axe";

afterEach(() => {
  vi.unstubAllGlobals();
  vi.restoreAllMocks();
});

const BASE = "https://armature.example.com";
const START = "/api/v1/armature/oauth/start";
const REFUSAL = "This Armature registers connected apps only by hand. Ask an Armature administrator to add Stator under Settings, Connected apps.";
const SECRET = "armature_ocs_abcdefghijklmnopqrstuvwxyz0123456789ABCDEFG";

function account(over: Partial<ArmatureAccount> = {}, app: Partial<ArmatureAccount["app"]> = {}): ArmatureAccount {
  return {
    configured: true,
    baseUrl: BASE,
    connected: false,
    via: null,
    status: "not_connected",
    user: null,
    checkedAt: null,
    missing: [],
    ...over,
    app: { offered: true, pending: false, refusal: null, scopes: [], ...app },
  };
}

const viaApp = account(
  {
    connected: true,
    via: "app",
    status: "ok",
    user: { id: "0195f000-0000-7000-8000-0000000000b1", name: "Ada", email: "ada@acme.armature.test" },
    checkedAt: "2026-10-01T09:00:00Z",
  },
  { scopes: ["read", "issue.write"] },
);

function connection(app: Partial<ArmatureConnection["app"]> = {}): ArmatureConnection {
  return {
    baseUrl: BASE,
    installationId: null,
    webhookSecretSet: true,
    webhookUrl: "http://stator.test/api/v1/armature/webhook",
    webhookTopics: ["issue.created"],
    lastDelivery: null,
    connected: 0,
    updatedAt: "2026-10-01T08:00:00Z",
    partner: { status: "ok", meta: true, version: "1.4.0", capabilities: ["issue-embed@1", "oauth.partner-tokens@1"], missing: [] },
    app: {
      offered: true,
      status: "none",
      clientId: null,
      registeredBy: null,
      refusal: null,
      redirectUri: "http://stator.test/api/v1/armature/oauth/callback",
      scopes: ["read", "issue.write"],
      ...app,
    },
  };
}

describe("connecting one's account with one click", () => {
  it("offers Connect Armature where Armature offers connected apps, with the token form behind a disclosure", async () => {
    stubApi({ "GET /armature/account": { status: 200, body: { account: account() } } });
    const user = userEvent.setup();
    await renderAt("/settings/profile");
    const connect = await screen.findByRole("link", { name: "Connect Armature" });
    expect(connect).toHaveAttribute("href", START);
    expect(screen.queryByLabelText("Armature token")).toBeNull();
    expect(screen.queryByText(/An Armature administrator has to allow Stator first/)).toBeNull();
    expect(await axeViolations()).toEqual([]);

    const disclosure = screen.getByRole("button", { name: "Use a token instead" });
    expect(disclosure).toHaveAttribute("aria-expanded", "false");
    await user.click(disclosure);
    expect(disclosure).toHaveAttribute("aria-expanded", "true");
    expect(await screen.findByLabelText("Armature token")).toBeInTheDocument();
  });

  it("carries the way back to Armature and says so", async () => {
    stubApi({ "GET /armature/account": { status: 200, body: { account: account() } } });
    await renderAt("/settings/profile?return=%2Fsettings%2Fthemes%3Fx%3D1#armature");
    expect(await screen.findByRole("link", { name: "Connect Armature" })).toHaveAttribute("href", `${START}?return=%2Fsettings%2Fthemes%3Fx%3D1`);
    expect(screen.getByText("Once you allow it, you go back to the page you came from.")).toBeInTheDocument();
  });

  it("explains that an Armature administrator has to allow a pending app first", async () => {
    stubApi({ "GET /armature/account": { status: 200, body: { account: account({}, { pending: true }) } } });
    await renderAt("/settings/profile");
    expect(await screen.findByText(/An Armature administrator has to allow Stator first/)).toBeInTheDocument();
    expect(screen.getByRole("link", { name: "Connect Armature" })).toBeInTheDocument();
  });

  it("shows Armature's own sentence and the token form when it would not register Stator", async () => {
    stubApi({ "GET /armature/account": { status: 200, body: { account: account({}, { offered: false, refusal: REFUSAL }) } } });
    await renderAt("/settings/profile");
    expect(await screen.findByText(REFUSAL)).toBeInTheDocument();
    expect(screen.getByLabelText("Armature token")).toBeInTheDocument();
    expect(screen.queryByRole("link", { name: "Connect Armature" })).toBeNull();
  });

  it.each([
    ["denied", "You did not allow Stator in Armature, so nothing is connected."],
    ["expired", "That return from Armature did not belong to this session, or took too long."],
    ["unreachable", "Armature did not answer. Try again in a moment."],
  ])("says how a connect that came back %s ended", async (outcome, sentence) => {
    stubApi({ "GET /armature/account": { status: 200, body: { account: account() } } });
    await renderAt(`/settings/profile?armature=${outcome}#armature`);
    expect(await screen.findByText(sentence, { exact: false })).toBeInTheDocument();
    expect(document.querySelector(`[data-armature-outcome="${outcome}"]`)).not.toBeNull();
  });

  it("says a refused registration in Armature's own words", async () => {
    stubApi({ "GET /armature/account": { status: 200, body: { account: account({}, { offered: false, refusal: REFUSAL }) } } });
    await renderAt("/settings/profile?armature=refused#armature");
    await waitFor(() => expect(document.querySelector('[data-armature-outcome="refused"]')).toHaveTextContent(REFUSAL));
  });

  it("ignores an outcome it does not know", async () => {
    stubApi({ "GET /armature/account": { status: 200, body: { account: account() } } });
    await renderAt("/settings/profile?armature=%3Cscript%3E#armature");
    expect(await screen.findByRole("link", { name: "Connect Armature" })).toBeInTheDocument();
    expect(document.querySelector("[data-armature-outcome]")).toBeNull();
  });

  it("goes back where the person was once connected, with the notice", async () => {
    stubApi({ "GET /armature/account": { status: 200, body: { account: viaApp } } });
    const router = await renderAt("/settings/profile?armature=connected&return=%2Fsettings%2Fthemes%3Fx%3D1#armature");
    await arrival(router, "/settings/themes");
    expect(router.state.location.href).toBe("/settings/themes?x=1");
    expect(await screen.findByText("Your Armature account is connected, and here is the page you came from.")).toBeInTheDocument();
  });

  it("says it is connected when there is no way back", async () => {
    stubApi({ "GET /armature/account": { status: 200, body: { account: viaApp } } });
    await renderAt("/settings/profile?armature=connected#armature");
    expect(await screen.findByText("Your Armature account is connected.")).toBeInTheDocument();
    expect(screen.getByText("Connected through Armature's connected apps.")).toBeInTheDocument();
  });

  it("says when Armature ended the connection, and disconnects after asking about Armature too", async () => {
    let current: ArmatureAccount = { ...viaApp, status: "rejected" };
    const sent = stubApi({
      "GET /armature/account": () => ({ status: 200, body: { account: current } }),
      "DELETE /armature/account/token": () => {
        current = account();
        return { status: 204 };
      },
    });
    const confirm = vi.spyOn(window, "confirm").mockReturnValue(true);
    const user = userEvent.setup();
    await renderAt("/settings/profile");
    expect(await screen.findByText(/Armature ended this connection/)).toBeInTheDocument();
    expect(screen.getByRole("link", { name: "Connect Armature" })).toBeInTheDocument();
    await user.click(screen.getByRole("button", { name: "Disconnect" }));
    expect(confirm).toHaveBeenCalledWith(expect.stringContaining("Stator also ends its access in Armature"));
    expect(await screen.findByText("Your Armature account is disconnected.")).toBeInTheDocument();
    expect(sent.some((r) => r.method === "DELETE")).toBe(true);
  });
});

describe("a connect hint where Armature offers connected apps", () => {
  it("starts the one-click connect and comes back to where it was followed", async () => {
    stubApi({
      "GET /armature/connection": { status: 200, body: { connection: connection() } },
      "GET /armature/account": { status: 200, body: { account: account() } },
    });
    await renderAt("/settings/armature");
    const hint = await screen.findByRole("link", { name: "Connect your own account" });
    await waitFor(() => expect(hint).toHaveAttribute("href", `${START}?return=%2Fsettings%2Farmature`));
    expect(hint).toHaveAttribute("data-one-click");
    expect(screen.getByText(/connects their own Armature account with one click/)).toBeInTheDocument();
  });
});

describe("the connected app, for administrators", () => {
  it("says what Armature said when it would not register Stator, and pastes the app registered by hand", async () => {
    let current = connection({ status: "refused", refusal: REFUSAL, registeredBy: "stator" });
    const sent = stubApi({
      "GET /armature/connection": () => ({ status: 200, body: { connection: current } }),
      "GET /armature/account": { status: 200, body: { account: account({}, { offered: false, refusal: REFUSAL }) } },
      "PUT /armature/connection/app": () => {
        current = connection({ status: "active", clientId: "0199e1c2-0000-7000-8000-000000000001", registeredBy: "admin" });
        return { status: 200, body: { connection: current } };
      },
    });
    const user = userEvent.setup();
    await renderAt("/settings/armature");
    expect(await screen.findByText(REFUSAL)).toBeInTheDocument();
    expect(screen.getByLabelText("Return address")).toHaveValue("http://stator.test/api/v1/armature/oauth/callback");
    expect(await axeViolations()).toEqual([]);

    await user.type(screen.getByLabelText("Client id"), "0199e1c2-0000-7000-8000-000000000001");
    await user.type(screen.getByLabelText("Client secret"), SECRET);
    await user.click(screen.getByRole("button", { name: "Save app" }));
    expect(await screen.findByText("The app is saved.")).toBeInTheDocument();
    expect(sent.find((r) => r.method === "PUT")?.body).toEqual({ clientId: "0199e1c2-0000-7000-8000-000000000001", clientSecret: SECRET });
    expect(screen.getByText("Stator is allowed in Armature.")).toBeInTheDocument();
    expect(screen.getByText(/An administrator pasted it/)).toBeInTheDocument();
    expect(document.body).not.toHaveTextContent(SECRET);
  });

  it("shows a refusal of the pasted app by its field", async () => {
    const SENTENCE = "That is not an Armature client secret. Paste the secret Armature showed once for the app.";
    stubApi({
      "GET /armature/connection": { status: 200, body: { connection: connection({ status: "refused", refusal: REFUSAL }) } },
      "GET /armature/account": { status: 200, body: { account: account() } },
      "PUT /armature/connection/app": {
        status: 422,
        body: { error: { code: "validation_failed", message: "Some fields need attention.", fields: { clientSecret: SENTENCE } } },
      },
    });
    const user = userEvent.setup();
    await renderAt("/settings/armature");
    await user.type(await screen.findByLabelText("Client id"), "abc");
    await user.type(screen.getByLabelText("Client secret"), "hunter2");
    await user.click(screen.getByRole("button", { name: "Save app" }));
    expect(await screen.findByText(SENTENCE)).toBeInTheDocument();
    expect(screen.getByLabelText("Client secret")).toHaveAttribute("aria-invalid", "true");
  });

  it("says a pending app waits for an Armature administrator, and forgets it after asking", async () => {
    let current = connection({ status: "pending", clientId: "0199e1c2-0000-7000-8000-000000000002", registeredBy: "stator" });
    stubApi({
      "GET /armature/connection": () => ({ status: 200, body: { connection: current } }),
      "GET /armature/account": { status: 200, body: { account: account({}, { pending: true }) } },
      "DELETE /armature/connection/app": () => {
        current = connection();
        return { status: 204 };
      },
    });
    vi.spyOn(window, "confirm").mockReturnValue(true);
    const user = userEvent.setup();
    await renderAt("/settings/armature");
    expect(await screen.findByText(/waits for an Armature administrator to allow it/)).toBeInTheDocument();
    expect(screen.getByText(/Stator registered itself/)).toBeInTheDocument();
    expect(screen.queryByLabelText("Client id")).toBeNull();
    await user.click(screen.getByRole("button", { name: "Forget app" }));
    expect(await screen.findByText(/The app is forgotten/)).toBeInTheDocument();
    expect(await screen.findByText("Stator registers itself in Armature when the first member connects.")).toBeInTheDocument();
  });

  it("says an Armature without connected apps keeps the token form", async () => {
    stubApi({
      "GET /armature/connection": { status: 200, body: { connection: connection({ offered: false }) } },
      "GET /armature/account": { status: 200, body: { account: account({}, { offered: false }) } },
    });
    await renderAt("/settings/armature");
    expect(await screen.findByText("This Armature does not offer connected apps, so members paste a token.")).toBeInTheDocument();
  });
});
