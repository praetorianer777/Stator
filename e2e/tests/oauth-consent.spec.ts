import { createHash, randomBytes } from "node:crypto";
import type { Page } from "@playwright/test";
import { authTest as test, expect, USERS } from "../fixtures/auth";
import { expectAccessible } from "../fixtures/shell";
import { uniqueName } from "../fixtures/seed";
import { WEB_URL } from "../fixtures/stack";

// Connecting an app to the MCP endpoint as a person (#449): the app registers
// itself, a member is told an administrator must allow it, alice (an
// administrator) allows it on the consent page, and its token works at MCP
// and nowhere else. The callback is a loopback address nobody listens on, so
// the browser's arrival there is answered here.
const CALLBACK = "http://127.0.0.1:47149/callback";
const MCP = `${WEB_URL}/api/v1/mcp`;

function pkce(): { verifier: string; challenge: string } {
  const verifier = randomBytes(32).toString("base64url");
  return { verifier, challenge: createHash("sha256").update(verifier).digest("base64url") };
}

async function register(name: string): Promise<string> {
  const response = await fetch(`${WEB_URL}/api/v1/oauth/register`, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({
      client_name: name,
      redirect_uris: [CALLBACK],
      token_endpoint_auth_method: "none",
      grant_types: ["authorization_code", "refresh_token"],
      scope: "read write",
    }),
  });
  expect(response.status).toBe(201);
  const body = (await response.json()) as { client_id: string; client_secret?: string };
  expect(body.client_secret).toBeUndefined();
  return body.client_id;
}

async function mcp(token: string, method: string, params?: object): Promise<Response> {
  return fetch(MCP, {
    method: "POST",
    headers: { Authorization: `Bearer ${token}`, "Content-Type": "application/json" },
    body: JSON.stringify({ jsonrpc: "2.0", id: 1, method, params }),
  });
}

/** Opens the authorization endpoint the way an app sends the browser there. */
async function authorize(page: Page, clientId: string, challenge: string, state: string, resource: string) {
  const query = new URLSearchParams({
    response_type: "code",
    client_id: clientId,
    redirect_uri: CALLBACK,
    scope: "read write",
    state,
    code_challenge: challenge,
    code_challenge_method: "S256",
    resource,
  });
  await page.goto(`/api/v1/oauth/authorize?${query}`);
}

test.describe("connecting an app to MCP with OAuth", { tag: ["@auth", "@desktop"] }, () => {
  test("a member waits for an administrator, who allows the app, whose token then works at MCP alone", async ({ page, pageAs }, testInfo) => {
    const name = uniqueName(testInfo, "assistant");
    const clientId = await register(name);
    const metadata = (await (await fetch(`${WEB_URL}/.well-known/oauth-protected-resource/api/v1/mcp`)).json()) as {
      resource: string;
      authorization_servers: string[];
    };
    const { verifier, challenge } = pkce();

    const bob = await pageAs("bob");
    await authorize(bob, clientId, challenge, "bob-state", metadata.resource);
    await expect(bob.getByRole("heading", { level: 1, name: `Connect ${name}` })).toBeVisible();
    await expect(bob.locator("[data-consent-refusal]")).toContainText(`A Stator administrator must allow ${name} first.`);
    await expect(bob.getByRole("button", { name: "Allow" })).toHaveCount(0);

    await page.route(`${CALLBACK}**`, (route) => route.fulfill({ status: 200, contentType: "text/plain", body: "connected" }));
    await authorize(page, clientId, challenge, "alice-state", metadata.resource);
    await expect(page.getByRole("heading", { level: 1, name: `Connect ${name}` })).toBeVisible();
    await expect(page.locator("[data-consent-allows-app]")).toBeVisible();
    await expect(page.locator("[data-consent-host]")).toHaveText("Afterwards you are sent back to 127.0.0.1:47149.");
    await expectAccessible(page);

    await page.locator('[data-action="consent-allow"]').click();
    await page.waitForURL(`${CALLBACK}**`);
    const back = new URL(page.url());
    expect(back.searchParams.get("state")).toBe("alice-state");
    expect(back.searchParams.get("iss")).toBe(metadata.authorization_servers[0]);
    const code = back.searchParams.get("code") ?? "";
    expect(code).toMatch(/^stator_oac_/);

    const redeemed = await fetch(`${WEB_URL}/api/v1/oauth/token`, {
      method: "POST",
      headers: { "Content-Type": "application/x-www-form-urlencoded" },
      body: new URLSearchParams({
        grant_type: "authorization_code",
        code,
        redirect_uri: CALLBACK,
        code_verifier: verifier,
        client_id: clientId,
        resource: metadata.resource,
      }),
    });
    expect(redeemed.status).toBe(200);
    const tokens = (await redeemed.json()) as { access_token: string; refresh_token: string; scope: string; expires_in: number };
    expect(tokens.scope).toBe("read write");
    expect(tokens.expires_in).toBeLessThanOrEqual(3600);

    const me = await mcp(tokens.access_token, "tools/call", { name: "whoami", arguments: {} });
    expect(me.status).toBe(200);
    const result = (await me.json()).result as { structuredContent?: { user?: { email: string } } };
    expect(result.structuredContent?.user?.email).toContain(USERS.alice.username);

    const elsewhere = await fetch(`${WEB_URL}/api/v1/tokens`, { headers: { Authorization: `Bearer ${tokens.access_token}` } });
    expect(elsewhere.status).toBe(401);

    // Now allowed, the app is offered to bob too.
    await authorize(bob, clientId, challenge, "bob-again", metadata.resource);
    await expect(bob.getByRole("button", { name: "Allow" })).toBeVisible();
    await expect(bob.locator("[data-consent-refusal]")).toHaveCount(0);
  });

  test("a 401 at MCP says where to connect", async () => {
    const answer = await mcp("stator_pat_nothing", "tools/list");
    expect(answer.status).toBe(401);
    expect(answer.headers.get("www-authenticate")).toMatch(
      /^Bearer resource_metadata="[^"]+\/\.well-known\/oauth-protected-resource\/api\/v1\/mcp", scope="read write", error="invalid_token"$/,
    );
  });
});
