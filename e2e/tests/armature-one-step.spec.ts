import type { APIRequestContext, Page } from "@playwright/test";
import { must } from "../fixtures/api";
import { expect } from "../fixtures/auth";
import { teamTest as test } from "../fixtures/team";
import { expectAccessible, confirm, dismissToasts } from "../fixtures/shell";
import { armatureURL } from "../fixtures/stack";

// Connecting Armature as an administrator in one step (#429), through the
// stub's consent page and its partner webhook. The stub offers both
// capabilities when reached by the host below, which the api's backchannel
// maps to it; Stator gives it the api's address on the stack's network,
// where the stub's ping and deliveries reach the real receiver.
const CAPABILITIES = [
  "calendar@1",
  "issue-embed@1",
  "plan@1",
  "remote-link-relations@1",
  "remote-links@1",
  "reports@1",
  "theme-export@1",
  "webhooks@1",
  "oauth.partner-tokens@1",
  "webhooks.partner@1",
];
/** Where the stack tells Armature to post, STATOR_ARMATURE_WEBHOOK_URL. */
const RECEIVER = "http://api:8080/api/v1/armature/webhook";

function oneStep(): { base: string; host: string } {
  const url = new URL(armatureURL());
  url.hostname = "partner-webhook.localhost";
  return { base: url.origin, host: url.host };
}

async function stub(request: APIRequestContext, method: string, path: string, data?: unknown) {
  const answer = await request.fetch(`${armatureURL()}${path}`, { method, data });
  expect(answer.status(), `${method} ${path}`).toBeLessThan(300);
  return answer.status() === 204 ? null : answer.json();
}

interface Hook {
  clientId: string;
  url: string;
  secret: string;
  topics: string[];
}

async function hookOf(request: APIRequestContext, clientId: string): Promise<Hook | undefined> {
  const body = (await stub(request, "GET", "/_stub-oauth/webhooks")) as { webhooks: Hook[] };
  return body.webhooks.find((each) => each.clientId === clientId);
}

/** Allows Stator on the stub's consent page as person of tenant. */
async function allow(page: Page, tenant: string, person: string) {
  await expect(page).toHaveURL(new RegExp(`^${oneStep().base}/oauth/authorize\\?`));
  await page.getByLabel("Armature organization").fill(tenant);
  await page.getByLabel("Your name in Armature").fill(person);
  await page.getByRole("button", { name: "Allow" }).click();
}

test.describe("connecting Armature in one step", { tag: ["@auth"] }, () => {
  test.beforeEach(async ({ api, request }) => {
    await stub(request, "PUT", `/_stub-meta/${oneStep().host}`, { capabilities: CAPABILITIES });
    await stub(request, "PUT", `/_stub-oauth/hosts/${oneStep().host}`, { registration: "approve" });
    await stub(request, "PUT", `/_stub-oauth/hosts/${oneStep().host}/outbound`, { refuse: "" });
    await api.DELETE("/armature/connection");
    must(await api.PUT("/armature/connection", { body: { baseUrl: oneStep().base } }));
  });

  test.afterEach(async ({ api, request }) => {
    await api.DELETE("/armature/connection");
    await stub(request, "DELETE", `/_stub-meta/${oneStep().host}`);
  });

  test("an administrator connects once, Armature makes the webhook, and an issue change reaches Stator", async ({ page, api, request, team }) => {
    await page.goto("/admin/armature");
    const section = page.locator("[data-armature-one-step]");
    await expect(section.getByRole("heading", { name: "Connect in one step" })).toBeVisible();
    await expect(page.getByLabel("Webhook secret", { exact: true })).toHaveCount(0);
    await expectAccessible(page);

    await section.locator('[data-action="connect-armature-webhook"]').click();
    await allow(page, team.label, "admin");
    await expect(page).toHaveURL(/\/admin\/armature\?armature=connected$/);
    await expect(page.getByText("Armature is connected: Stator made its webhook there, and your own account is connected.")).toBeVisible();
    await expect(section).toHaveAttribute("data-armature-one-step", "connected");
    await expect(section.locator("[data-armature-webhook-made]")).toContainText(RECEIVER);
    await expectAccessible(page);

    const connection = (await api.GET("/armature/connection")).data!.connection!;
    expect(connection.webhookSecretSet).toBe(true);
    const clientId = connection.app.clientId!;
    const hook = await hookOf(request, clientId);
    expect(hook, "the webhook Stator made in the stub's Armature").toMatchObject({ url: RECEIVER });
    expect(hook!.topics).toEqual([...connection.webhookTopics].sort());
    await expect(page.locator("body")).not.toContainText(hook!.secret);

    // Armature's ping goes through the real receiver, which holds it to the stored secret.
    await section.locator('[data-action="test-armature-webhook"]').click();
    await expect(section.locator("[data-armature-webhook-said]")).toHaveText("Armature reached Stator, which answered 204.");
    await page.reload();
    await expect(page.locator("[data-last-delivery]")).toContainText("Stator last took a delivery, ping,");
    await section.locator('[data-action="rotate-armature-webhook"]').click();
    await confirm(page);
    await expect(section.locator("[data-armature-webhook-said]")).toHaveText("The webhook has a new secret, which Stator stored.");
    const rotated = await hookOf(request, clientId);
    expect(rotated!.secret).not.toBe(hook!.secret);

    // An issue change, signed with the secret Armature holds now, is taken.
    const sent = (await stub(request, "POST", `/_stub/${team.label}/webhooks`, {
      url: RECEIVER,
      secret: rotated!.secret,
      topic: "issue.updated",
      payload: { key: "CP-1" },
    })) as { status: number };
    expect(sent.status).toBe(204);
    const stale = (await stub(request, "POST", `/_stub/${team.label}/webhooks`, {
      url: RECEIVER,
      secret: hook!.secret,
      topic: "issue.updated",
      payload: { key: "CP-1" },
    })) as { status: number };
    expect(stale.status).toBe(401);

    // Disconnecting removes the webhook from Armature.
    await dismissToasts(page);
    await page.locator('[data-action="disconnect-armature"]').click();
    await confirm(page);
    await expect(page.getByText("Armature is disconnected.")).toBeVisible();
    await expect.poll(async () => hookOf(request, clientId)).toBeUndefined();
  });

  test("Armature's outbound guard refusing Stator's address is said in Armature's words", async ({ page, request, team }) => {
    await stub(request, "PUT", `/_stub-oauth/hosts/${oneStep().host}/outbound`, { refuse: "outbound_refused" });
    await page.goto("/admin/armature");
    await page.locator('[data-action="connect-armature-webhook"]').click();
    await allow(page, team.label, "admin");
    await expect(page).toHaveURL(/\/admin\/armature\?armature=webhook_refused$/);
    await expect(page.locator('[data-armature-outcome="webhook_refused"]')).toBeVisible();
    await expect(page.locator("[data-armature-webhook-refusal]")).toContainText("Add api to network.outboundAllow (ARMATURE_OUTBOUND_ALLOW)");
    await expect(page.locator('[data-action="connect-armature-webhook"]')).toHaveText("Connect again");
    await expectAccessible(page);
  });
});
