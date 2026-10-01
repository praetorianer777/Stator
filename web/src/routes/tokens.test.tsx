import { afterEach, describe, expect, it, vi } from "vitest";
import { screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import type { ApiToken } from "@/api/tokens";
import { DAY_MS, MCP_PATH, TOKEN_DEFAULT_EXPIRY_DAYS } from "@/config";
import { renderAt, stubApi } from "@/test/app";
import { axeViolations } from "@/test/axe";
import { expiryFrom } from "./tokens";

afterEach(() => {
  vi.unstubAllGlobals();
  vi.restoreAllMocks();
});

const SECRET = "stator_pat_AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA";

function token(over: Partial<ApiToken>): ApiToken {
  return {
    id: "0195f000-0000-7000-8000-0000000000a1",
    name: "deploy",
    scopes: [],
    lastUsedAt: null,
    expiresAt: null,
    createdAt: "2026-09-29T08:00:00Z",
    ...over,
  };
}

const deploy = token({ lastUsedAt: "2026-09-29T09:30:00Z", expiresAt: "2099-01-01T00:00:00Z" });
const reporting = token({ id: "0195f000-0000-7000-8000-0000000000a2", name: "reporting", scopes: ["read"] });
const stale = token({ id: "0195f000-0000-7000-8000-0000000000a3", name: "stale", expiresAt: "2020-01-01T00:00:00Z" });

const row = (name: string) => document.querySelector(`[data-token-row="${name}"]`) as HTMLElement;

describe("the tokens page", () => {
  it("lists each token with its last use and expiry, never its secret", async () => {
    stubApi({ "GET /tokens": { status: 200, body: { tokens: [deploy, reporting, stale] } } });
    await renderAt("/settings/tokens");
    expect(await screen.findByRole("heading", { level: 1, name: "Personal access tokens" })).toBeInTheDocument();
    await waitFor(() => expect(row("deploy")).not.toBeNull());

    expect(row("deploy").querySelector("[data-token-last-used]")).not.toHaveTextContent("Never used");
    expect(row("reporting").querySelector("[data-token-last-used]")).toHaveTextContent("Never used");
    expect(row("reporting").querySelector("[data-token-expires]")).toHaveTextContent("Never");
    expect(within(row("reporting")).getByText("Read only")).toBeInTheDocument();
    expect(within(row("stale")).getByText("Expired")).toBeInTheDocument();
    expect(within(row("deploy")).queryByText("Expired")).toBeNull();
    expect(document.querySelector("[data-fresh-token]")).toBeNull();
  });

  it("says how to connect an assistant, with the address and client settings", async () => {
    stubApi({ "GET /tokens": { status: 200, body: { tokens: [] } } });
    await renderAt("/settings/tokens");
    expect(await screen.findByRole("heading", { level: 2, name: "Connect an assistant" })).toBeInTheDocument();
    const endpoint = `${window.location.origin}${MCP_PATH}`;
    expect(screen.getByLabelText("MCP address")).toHaveValue(endpoint);
    const settings = JSON.parse(document.querySelector("[data-mcp-config]")?.textContent ?? "{}");
    expect(settings.mcpServers.stator).toEqual({ type: "http", url: endpoint, headers: { Authorization: "Bearer <your token>" } });
    expect(screen.getByText(/only the tools that read/)).toBeInTheDocument();
    expect(await axeViolations()).toEqual([]);
  });

  it("says what a token is for when there is none", async () => {
    stubApi({ "GET /tokens": { status: 200, body: { tokens: [] } } });
    await renderAt("/settings/tokens");
    expect(await screen.findByText("No tokens yet")).toBeInTheDocument();
  });

  it("creates a token and shows its secret once, with a way to copy it", async () => {
    let tokens: ApiToken[] = [];
    const sent = stubApi({
      "GET /tokens": () => ({ status: 200, body: { tokens } }),
      "POST /tokens": () => {
        const made = token({ name: "reporting", scopes: ["read"] });
        tokens = [made];
        return { status: 201, body: { token: { ...made, secret: SECRET } } };
      },
    });
    const user = userEvent.setup();
    await renderAt("/settings/tokens");
    await user.type(await screen.findByLabelText("Token name"), "reporting");
    await user.click(screen.getByLabelText("Read only"));
    await user.selectOptions(screen.getByLabelText("Expires"), "Never");
    await user.click(screen.getByRole("button", { name: "Create token" }));

    const secret = await screen.findByLabelText("Your new token");
    expect(secret).toHaveValue(SECRET);
    expect(sent.find((r) => r.method === "POST")?.body).toEqual({ name: "reporting", scopes: ["read"] });
    expect(screen.getByLabelText("Token name")).toHaveValue("");

    await user.click(screen.getByRole("button", { name: "Copy token" }));
    expect(await navigator.clipboard.readText()).toBe(SECRET);
    expect(await screen.findByText("Copied the token.")).toBeInTheDocument();

    await waitFor(() => expect(row("reporting")).not.toBeNull());
    expect(row("reporting")).not.toHaveTextContent(SECRET);

    await user.click(screen.getByRole("button", { name: "Done" }));
    expect(screen.queryByLabelText("Your new token")).toBeNull();
    expect(document.body).not.toHaveTextContent(SECRET);
  });

  it("gives a new token the default lifetime unless told otherwise", async () => {
    const sent = stubApi({
      "GET /tokens": { status: 200, body: { tokens: [] } },
      "POST /tokens": { status: 201, body: { token: { ...deploy, secret: SECRET } } },
    });
    const user = userEvent.setup();
    await renderAt("/settings/tokens");
    await user.type(await screen.findByLabelText("Token name"), "deploy");
    const before = Date.now();
    await user.click(screen.getByRole("button", { name: "Create token" }));
    await waitFor(() => expect(sent.some((r) => r.method === "POST")).toBe(true));
    const body = sent.find((r) => r.method === "POST")?.body as { expiresAt: string; scopes: string[] };
    expect(body.scopes).toEqual([]);
    const lifetime = new Date(body.expiresAt).getTime() - before;
    expect(lifetime).toBeGreaterThanOrEqual(TOKEN_DEFAULT_EXPIRY_DAYS * DAY_MS - 1000);
    expect(lifetime).toBeLessThanOrEqual(TOKEN_DEFAULT_EXPIRY_DAYS * DAY_MS + 60_000);
  });

  it("works out an expiry from days, and none for never", () => {
    const now = new Date("2026-09-29T12:00:00Z");
    expect(expiryFrom(7, now)).toBe("2026-10-06T12:00:00.000Z");
    expect(expiryFrom(0, now)).toBeUndefined();
  });

  it("asks for a name before asking the server", async () => {
    const sent = stubApi({ "GET /tokens": { status: 200, body: { tokens: [] } } });
    const user = userEvent.setup();
    await renderAt("/settings/tokens");
    await user.click(await screen.findByRole("button", { name: "Create token" }));
    expect(await screen.findByText("Give the token a name, so you know later what uses it.")).toBeInTheDocument();
    expect(sent.some((r) => r.method === "POST")).toBe(false);
  });

  it("shows the server's word on a field it refused", async () => {
    stubApi({
      "GET /tokens": { status: 200, body: { tokens: [] } },
      "POST /tokens": {
        status: 422,
        body: { error: { code: "validation_failed", message: "Some fields need attention.", fields: { expiresAt: "The expiry has to be in the future." } } },
      },
    });
    const user = userEvent.setup();
    await renderAt("/settings/tokens");
    await user.type(await screen.findByLabelText("Token name"), "deploy");
    await user.click(screen.getByRole("button", { name: "Create token" }));
    expect(await screen.findByText("The expiry has to be in the future.")).toBeInTheDocument();
  });

  it("revokes a token once the reader confirms", async () => {
    let tokens = [deploy, reporting];
    const sent = stubApi({
      "GET /tokens": () => ({ status: 200, body: { tokens } }),
      [`DELETE /tokens/${deploy.id}`]: () => {
        tokens = [reporting];
        return { status: 204 };
      },
    });
    const confirm = vi.spyOn(window, "confirm").mockReturnValueOnce(false).mockReturnValueOnce(true);
    const user = userEvent.setup();
    await renderAt("/settings/tokens");
    const revoke = await screen.findByRole("button", { name: "Revoke deploy" });

    await user.click(revoke);
    expect(sent.some((r) => r.method === "DELETE")).toBe(false);

    await user.click(revoke);
    expect(confirm).toHaveBeenLastCalledWith("Revoke deploy? Anything using it is refused from now on.");
    expect(await screen.findByText("deploy was revoked.")).toBeInTheDocument();
    await waitFor(() => expect(row("deploy")).toBeNull());
    expect(sent.filter((r) => r.method === "DELETE").map((r) => r.path)).toEqual([`/tokens/${deploy.id}`]);
  });

  it("is reached from the account menu", async () => {
    stubApi({ "GET /tokens": { status: 200, body: { tokens: [] } } });
    const user = userEvent.setup();
    const router = await renderAt("/");
    await user.click(screen.getByRole("button", { name: "Your account" }));
    await user.click(await screen.findByRole("menuitem", { name: "Access tokens" }));
    await waitFor(() => expect(router.state.location.pathname).toBe("/settings/tokens"));
  });

  it("passes axe, with a fresh secret on show", async () => {
    stubApi({
      "GET /tokens": { status: 200, body: { tokens: [deploy, reporting, stale] } },
      "POST /tokens": { status: 201, body: { token: { ...deploy, secret: SECRET } } },
    });
    const user = userEvent.setup();
    await renderAt("/settings/tokens");
    await user.type(await screen.findByLabelText("Token name"), "deploy");
    await user.click(screen.getByRole("button", { name: "Create token" }));
    await screen.findByLabelText("Your new token");
    expect(await axeViolations()).toEqual([]);
  });
});
