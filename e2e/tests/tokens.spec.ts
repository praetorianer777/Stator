import type { Page } from "@playwright/test";
import { authTest as test, expect, ME_PATH, USERS } from "../fixtures/auth";
import { ONE_LOOK, openUntil } from "../fixtures/replica";
import { expectAccessible, openShell } from "../fixtures/shell";
import { uniqueName } from "../fixtures/seed";
import { WEB_URL } from "../fixtures/stack";

const TOKENS_PATH = "/settings/tokens";
const TOKEN_PREFIX = "stator_pat_";
const row = (page: Page, name: string) => page.locator(`[data-token-row="${name}"]`);

/** Calls the API the way a script would: Node's own fetch, the token as a bearer, no cookies. */
function asScript(secret: string, path = ME_PATH): Promise<Response> {
  return fetch(`${WEB_URL}${path}`, { headers: { Authorization: `Bearer ${secret}` } });
}

type ToolResult = { isError?: boolean; content: { text: string }[]; structuredContent?: { user?: { email: string } } };

/** One JSON-RPC call to the MCP endpoint, the way an assistant's client makes it. */
async function mcp<T>(endpoint: string, secret: string, method: string, params?: object): Promise<T> {
  const response = await fetch(endpoint, {
    method: "POST",
    headers: { Authorization: `Bearer ${secret}`, "Content-Type": "application/json" },
    body: JSON.stringify({ jsonrpc: "2.0", id: 1, method, params }),
  });
  expect(response.status).toBe(200);
  return (await response.json()).result as T;
}

async function makeToken(page: Page, name: string, readOnly: boolean): Promise<string> {
  await page.getByLabel("Token name", { exact: true }).fill(name);
  if (readOnly) await page.getByLabel("Read only", { exact: true }).check();
  await page.locator('[data-action="create-token"]').click();
  const secret = await page.getByLabel("Your new token", { exact: true }).inputValue();
  await page.locator('[data-action="token-done"]').click();
  return secret;
}

test.describe("personal access tokens", { tag: ["@auth", "@desktop"] }, () => {
  // Only this worker's, since the specs run side by side as the same person
  // and one revoking the other's token mid-test would fail it.
  test.afterEach(async ({ api }, testInfo) => {
    const ours = uniqueName(testInfo, "").split("-")[0];
    const { data } = await api.GET("/tokens");
    for (const token of data?.tokens ?? []) {
      if (token.name.startsWith(`${ours}-`)) await api.DELETE("/tokens/{tokenID}", { params: { path: { tokenID: token.id } } });
    }
  });

  test("a token is made, shown once, works from a script and stops when revoked", async ({ page, context }, testInfo) => {
    const name = uniqueName(testInfo, "script");
    await context.grantPermissions(["clipboard-read", "clipboard-write"]);

    await openShell(page, "/");
    await page.locator('[data-action="account"]').click();
    await page.getByRole("menu", { name: "Your account" }).locator('[data-action="tokens"]').click();
    await expect(page).toHaveURL(new RegExp(`${TOKENS_PATH}$`));
    await expect(page.getByRole("heading", { level: 1, name: "Personal access tokens" })).toBeVisible();

    await page.getByLabel("Token name", { exact: true }).fill(name);
    await page.locator('[data-action="create-token"]').click();
    const shown = page.getByLabel("Your new token", { exact: true });
    await expect(shown).toHaveValue(new RegExp(`^${TOKEN_PREFIX}[A-Za-z0-9_-]{43}$`));
    const secret = await shown.inputValue();

    await page.locator('[data-action="copy-token"]').click();
    await expect(page.locator("[data-copy-status]")).toHaveText("Copied the token.");
    expect(await page.evaluate(() => navigator.clipboard.readText())).toBe(secret);
    await expectAccessible(page);

    const me = await asScript(secret);
    expect(me.status).toBe(200);
    expect((await me.json()).user.email).toContain(USERS.alice.username);

    await page.locator('[data-action="token-done"]').click();
    await expect(shown).toHaveCount(0);
    // The last use is written as the script calls, not by this session, so this read is not held to it.
    await openUntil(page, TOKENS_PATH, async () => {
      await expect(row(page, name)).toBeVisible(ONE_LOOK);
      await expect(row(page, name).locator("[data-token-last-used]")).not.toHaveText("Never used", ONE_LOOK);
    });
    await expect(page.locator("body")).not.toContainText(secret);
    await expect(row(page, name).locator("[data-token-expires]")).not.toHaveText("Never");

    page.once("dialog", (dialog) => dialog.accept());
    await row(page, name)
      .getByRole("button", { name: `Revoke ${name}` })
      .click();
    await expect(page.locator("[data-tokens-notice]")).toHaveText(`${name} was revoked.`);
    await expect(row(page, name)).toHaveCount(0);

    expect((await asScript(secret)).status).toBe(401);
  });

  test("a read-only token reads and is refused a write", async ({ page }, testInfo) => {
    const name = uniqueName(testInfo, "reader");
    await openShell(page, TOKENS_PATH);
    await page.getByLabel("Token name", { exact: true }).fill(name);
    await page.getByLabel("Read only", { exact: true }).check();
    await page.getByLabel("Expires", { exact: true }).selectOption({ label: "Never" });
    await page.locator('[data-action="create-token"]').click();
    const secret = await page.getByLabel("Your new token", { exact: true }).inputValue();
    await expect(row(page, name)).toContainText("Read only");
    await expect(row(page, name).locator("[data-token-expires]")).toHaveText("Never");

    expect((await asScript(secret)).status).toBe(200);
    const write = await fetch(`${WEB_URL}/api/v1/themes`, {
      method: "POST",
      headers: { Authorization: `Bearer ${secret}`, "Content-Type": "application/json" },
      body: JSON.stringify({ name }),
    });
    expect(write.status).toBe(403);
    expect((await write.json()).error.code).toBe("read_only_token");
  });

  test("an assistant connects at the address shown and is offered what its token may do", async ({ page }, testInfo) => {
    await openShell(page, TOKENS_PATH);
    await expect(page.getByRole("heading", { level: 2, name: "Connect an assistant" })).toBeVisible();
    const endpoint = await page.getByLabel("MCP address", { exact: true }).inputValue();
    expect(endpoint).toBe(`${WEB_URL}/api/v1/mcp`);
    await expect(page.locator("[data-mcp-config]")).toContainText(endpoint);
    await expectAccessible(page);

    const writer = await makeToken(page, uniqueName(testInfo, "assistant"), false);
    const reader = await makeToken(page, uniqueName(testInfo, "reading-assistant"), true);

    const names = async (secret: string) => (await mcp<{ tools: { name: string }[] }>(endpoint, secret, "tools/list")).tools.map((tool) => tool.name);
    expect(await names(writer)).toEqual(expect.arrayContaining(["search", "get_page_markdown", "create_page", "replace_page_markdown"]));
    const readable = await names(reader);
    expect(readable).toEqual(expect.arrayContaining(["search", "get_page_markdown"]));
    expect(readable).not.toContain("create_page");

    const me = await mcp<ToolResult>(endpoint, reader, "tools/call", { name: "whoami", arguments: {} });
    expect(me.structuredContent?.user?.email).toContain(USERS.alice.username);
    const refused = await mcp<ToolResult>(endpoint, reader, "tools/call", {
      name: "create_page",
      arguments: { parentId: crypto.randomUUID(), title: "Not allowed" },
    });
    expect(refused.isError).toBe(true);
    expect(refused.content[0]?.text).toContain("can only read");
  });
});
