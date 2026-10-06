import { createHmac, timingSafeEqual } from "node:crypto";
import type { APIRequestContext, Page, TestInfo } from "@playwright/test";
import type { StatorApi } from "../fixtures/api";
import { expect } from "../fixtures/auth";
import { orgTest as test } from "../fixtures/org";
import { openShowing } from "../fixtures/replica";
import { uniqueName } from "../fixtures/seed";
import { expectAccessible, scrollsSideways, startInScheme, type ColourScheme } from "../fixtures/shell";
import { armatureURL } from "../fixtures/stack";

// web/src/config.ts WEBHOOKS_PATH and WEBHOOK_SIGNATURE_HEADER.
const WEBHOOKS_PATH = "/settings/webhooks";
const SIGNATURE_HEADER = "X-Stator-Signature-256";
const SECRET_PREFIX = "stator_whs_";
// The armature-stub keeps what each bin is sent; the api reaches it on the
// stack's network by this name, which STATOR_OUTBOUND_ALLOW lets through.
const STUB_FROM_API = "http://armature-stub:8080";

type Received = { headers: Record<string, string>; body: string };

/** A bin of its own for a test: where the api posts, and where the browser's side reads it back. */
function binFor(testInfo: TestInfo): { address: string; read: (request: APIRequestContext) => Promise<Received[]> } {
  const bin = `e2e-${testInfo.testId}-${testInfo.retry}-${testInfo.project.name}`.replace(/[^a-zA-Z0-9-]/g, "-");
  return {
    address: `${STUB_FROM_API}/_hooks/${bin}`,
    read: async (request) => {
      const answer = await request.get(`${armatureURL()}/_hooks/${bin}`);
      expect(answer.ok()).toBe(true);
      return ((await answer.json()) as { deliveries: Received[] }).deliveries;
    },
  };
}

/** Whether a body carries the signature the secret makes, as a receiver would check it. */
function signedWith(secret: string, delivery: Received): boolean {
  const expected = Buffer.from(`sha256=${createHmac("sha256", secret).update(delivery.body).digest("hex")}`);
  const got = Buffer.from(delivery.headers[SIGNATURE_HEADER] ?? "");
  return got.length === expected.length && timingSafeEqual(got, expected);
}

/** A webhook made through the API, for the specs that start from one. */
async function webhookFor(api: StatorApi, name: string, address: string): Promise<void> {
  const made = await api.POST("/webhooks", { body: { name, url: address, topics: ["*"] } });
  expect(made.response.status).toBe(201);
}

async function deleteWebhooks(api: StatorApi): Promise<void> {
  const listed = await api.GET("/webhooks");
  for (const hook of listed.data?.webhooks ?? []) {
    await api.DELETE("/webhooks/{webhookID}", { params: { path: { webhookID: hook.id } } });
  }
}

async function openWith(page: Page, name: string): Promise<void> {
  await openShowing(page, WEBHOOKS_PATH, page.locator(`[data-webhook="${name}"]`));
}

test.describe("outbound webhooks", { tag: ["@auth"] }, () => {
  test.afterEach(async ({ api }) => {
    await deleteWebhooks(api);
  });

  test("an administrator adds one, copies its secret once, pings it, reads the log and turns it off", async ({ page, request }, testInfo) => {
    const name = uniqueName(testInfo, "Chat");
    const bin = binFor(testInfo);

    await page.goto("/");
    await page.locator('[data-action="account"]').click();
    await page.locator('[role="menu"] [data-action="webhooks"]').click();
    await expect(page).toHaveURL(/\/settings\/webhooks$/);
    await expect(page.locator("main").getByRole("heading", { level: 1 })).toHaveText("Webhooks");

    await page.getByLabel("Name", { exact: true }).fill(name);
    await page.getByLabel("Address", { exact: true }).fill(bin.address);
    await page.getByLabel("Everything, events added later too").uncheck();
    await page.getByLabel("A page is published").check();
    await page.getByLabel("A comment is added").check();
    await page.locator('[data-action="create-webhook"]').click();

    const secretField = page.getByLabel(`Secret for ${name}`, { exact: true });
    await expect(secretField).toHaveValue(new RegExp(`^${SECRET_PREFIX}`));
    const secret = await secretField.inputValue();
    await expect(page.locator("[data-webhook-secret]")).toContainText(SIGNATURE_HEADER);
    await page.locator('[data-action="webhook-secret-done"]').click();
    await expect(page.locator("[data-webhook-secret]")).toHaveCount(0);

    const row = page.locator(`[data-webhook="${name}"]`);
    await expect(row).toContainText("A page is published, A comment is added");
    await expect(row).toHaveAttribute("data-webhook-enabled", "true");
    await page.reload();
    await expect(row).toBeVisible();
    expect(await page.content()).not.toContain(secret);

    await row.getByRole("button", { name: `Actions for ${name}` }).click();
    await page.getByRole("menuitem", { name: "Send a test ping" }).click();
    await expect(page.locator("[data-webhooks-notice]")).toHaveText(`The ping reached ${name}. The receiver answered 204.`);
    const received = await bin.read(request);
    expect(received).toHaveLength(1);
    expect(signedWith(secret, received[0]!)).toBe(true);
    expect(JSON.parse(received[0]!.body)).toMatchObject({ topic: "ping", payload: { message: "Stator can reach this address." } });

    await row.getByRole("button", { name: `Actions for ${name}` }).click();
    await page.getByRole("menuitem", { name: "Delivery log" }).click();
    const log = page.getByRole("dialog", { name: `Deliveries to ${name}` });
    await expect(log.locator('[data-delivery="ping"]')).toHaveAttribute("data-delivery-state", "delivered");
    await expect(log).toContainText("The receiver answered 204.");
    await log.getByRole("button", { name: /^Send again/ }).click();
    await expect(log).toContainText("Sent again: Delivered.");
    await expect(log.locator('[data-delivery="ping"]')).toHaveCount(2);
    await page.keyboard.press("Escape");
    await expect(log).toHaveCount(0);

    await row.getByRole("button", { name: `Actions for ${name}` }).click();
    await page.getByRole("menuitem", { name: "Turn off" }).click();
    await expect(row).toHaveAttribute("data-webhook-enabled", "false");
    await expect(row.locator("[data-webhook-state]")).toHaveText("Off");
    await expect(page.locator("[data-webhooks-notice]")).toHaveText(`${name} is off. Nothing is sent until it is turned on again.`);
  });

  test("bob, a member, is refused it", async ({ apiAs, pageAs }) => {
    const bobApi = await apiAs("bob");
    const refused = await bobApi.GET("/webhooks");
    expect(refused.response.status).toBe(403);
    expect((refused.error as { error?: { message?: string } } | undefined)?.error?.message).toMatch(
      /^Only an administrator of this organization can do that\. Ask one of them/,
    );

    const bob = await pageAs("bob");
    await bob.goto(WEBHOOKS_PATH);
    await expect(bob.locator("[data-webhooks-refused]")).toContainText("Only an administrator of this organization keeps its webhooks.");
    await expect(bob.locator("[data-webhook-form]")).toHaveCount(0);
    await bob.locator('[data-action="account"]').click();
    await expect(bob.getByRole("menu")).toBeVisible();
    await expect(bob.locator('[role="menu"] [data-action="webhooks"]')).toHaveCount(0);
  });

  test("it is added and pinged from the keyboard", { tag: ["@desktop"] }, async ({ page }, testInfo) => {
    const name = uniqueName(testInfo, "Keys");
    const bin = binFor(testInfo);
    await page.goto(WEBHOOKS_PATH);
    await page.getByLabel("Name", { exact: true }).focus();
    await page.keyboard.type(name);
    await page.keyboard.press("Tab");
    await expect(page.getByLabel("Address", { exact: true })).toBeFocused();
    await page.keyboard.type(bin.address);
    await page.keyboard.press("Tab");
    const everything = page.getByLabel("Everything, events added later too");
    await expect(everything).toBeFocused();
    await page.keyboard.press("Space");
    await expect(everything).not.toBeChecked();
    await page.keyboard.press("Tab");
    await expect(page.getByLabel("A page is published")).toBeFocused();
    await page.keyboard.press("Space");
    await expect(page.getByLabel("A page is published")).toBeChecked();
    await page.getByLabel("Address", { exact: true }).focus();
    await page.keyboard.press("Enter");
    await expect(page.getByLabel(`Secret for ${name}`, { exact: true })).toBeVisible();
    await expect(page.locator(`[data-webhook="${name}"]`)).toContainText("A page is published");

    const actions = page.getByRole("button", { name: `Actions for ${name}` });
    await actions.focus();
    await page.keyboard.press("Enter");
    await expect(page.getByRole("menuitem", { name: "Delivery log" })).toBeFocused();
    await page.keyboard.press("ArrowDown");
    await expect(page.getByRole("menuitem", { name: "Send a test ping" })).toBeFocused();
    await page.keyboard.press("Enter");
    await expect(page.locator("[data-webhooks-notice]")).toHaveText(`The ping reached ${name}. The receiver answered 204.`);
  });

  for (const scheme of ["light", "dark"] as ColourScheme[]) {
    test(`it passes axe in ${scheme}`, async ({ page, api }, testInfo) => {
      const name = uniqueName(testInfo, `Axe ${scheme}`);
      await webhookFor(api, name, binFor(testInfo).address);
      await startInScheme(page, scheme);
      await openWith(page, name);
      await expectAccessible(page);
      await page.getByRole("button", { name: `Actions for ${name}` }).click();
      await page.getByRole("menuitem", { name: "Send a test ping" }).click();
      await expect(page.locator("[data-webhooks-notice]")).not.toBeEmpty();
      await page.getByRole("button", { name: `Actions for ${name}` }).click();
      await page.getByRole("menuitem", { name: "Delivery log" }).click();
      await expect(page.locator('[data-delivery="ping"]')).toBeVisible();
      await expectAccessible(page);
    });
  }

  test("on a phone the form and the list fit and the page never scrolls sideways", { tag: ["@mobile"] }, async ({ page, api }, testInfo) => {
    const name = uniqueName(testInfo, "Phone");
    await webhookFor(api, name, binFor(testInfo).address);
    await openWith(page, name);
    await expect(page.getByLabel("Address", { exact: true })).toBeVisible();
    await expect(page.locator('[data-action="create-webhook"]')).toBeVisible();
    await expect(page.getByRole("button", { name: `Actions for ${name}` })).toBeVisible();
    expect(await scrollsSideways(page)).toBe(false);
    await page.getByRole("button", { name: `Actions for ${name}` }).click();
    await page.getByRole("menuitem", { name: "Delivery log" }).click();
    await expect(page.getByRole("dialog", { name: `Deliveries to ${name}` })).toBeVisible();
    expect(await scrollsSideways(page)).toBe(false);
  });
});
