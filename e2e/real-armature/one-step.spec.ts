import type { Page } from "@playwright/test";
import { expect } from "../fixtures/auth";
import { teamTest as test } from "../fixtures/team";
import { answered, armatureAs, armatureSignedIn, passwordOf, readSeed, realArmatureURL, type ArmaturePerson, WEBHOOK_URL } from "./armature";

// Connecting the real Armature as an administrator in one step (#429): one
// consent on Armature's own page with every scope, after which Armature
// holds Stator's own webhook and its worker posts issue changes to Stator,
// signed with the secret Armature answered and nobody copied.

/** Armature's worker picks a delivery up within seconds, not at once. */
const WORKER_DELIVERS = { timeout: 30_000 };
/** The consent, the webhook and two deliveries by Armature's worker take longer than a spec's default. */
const ONE_STEP_TIMEOUT_MS = 90_000;
/** How far the api's clock may be behind the browser's when it stamps a delivery. */
const CLOCK_SLACK_MS = 1_000;

interface Endpoint {
  id: string;
  name: string;
  url: string;
  topics: string[];
  partner: { clientId: string; name: string } | null;
}

async function signInToArmature(page: Page, person: ArmaturePerson): Promise<void> {
  await expect(page).toHaveURL(new RegExp(`^${realArmatureURL()}/login\\?`));
  await page.getByLabel("Email", { exact: true }).fill(person.email);
  await page.getByLabel("Password", { exact: true }).fill(passwordOf(person.email));
  await page.getByRole("button", { name: "Sign in", exact: true }).click();
  await expect(page).toHaveURL(new RegExp(`^${realArmatureURL()}/oauth/authorize\\?`));
}

test.describe("connecting a real Armature in one step", { tag: ["@auth"] }, () => {
  test.beforeEach(async ({ api }) => {
    await api.DELETE("/armature/connection");
  });

  test.afterEach(async ({ api }) => {
    await api.DELETE("/armature/connection");
  });

  test("an administrator allows Stator once, Armature makes its webhook, and an issue change reaches Stator", async ({ api, page }) => {
    test.setTimeout(ONE_STEP_TIMEOUT_MS);
    const seed = readSeed();
    const { admin } = seed.people;
    expect((await api.PUT("/armature/connection", { body: { baseUrl: seed.url } })).response.status).toBe(200);

    await page.goto("/settings/armature");
    const section = page.locator("[data-armature-one-step]");
    await expect(section.getByRole("heading", { name: "Connect in one step" })).toBeVisible();
    await expect(page.getByLabel("Webhook secret", { exact: true })).toHaveCount(0);
    await section.locator('[data-action="connect-armature-webhook"]').click();

    await signInToArmature(page, admin);
    const consent = page.locator('[data-consent="Stator"]');
    await expect(consent).toContainText(`wants to act as ${admin.name} in Armature`);
    await expect(consent.locator('[data-consent-scope="webhooks.partner"]')).toBeVisible();
    await page.locator('[data-action="consent-allow"]').click();

    await expect(page).toHaveURL(/\/settings\/armature\?armature=connected$/);
    await expect(section).toHaveAttribute("data-armature-one-step", "connected");
    const connection = (await api.GET("/armature/connection")).data!.connection!;
    expect(connection.webhookSecretSet).toBe(true);
    expect(connection.partnerWebhook).toMatchObject({ connected: true, manageable: true, refusal: null, url: WEBHOOK_URL });
    const clientId = connection.app.clientId!;

    // The webhook is there in Armature, Stator's own, at the api's address.
    const armature = await armatureSignedIn(admin.email);
    const ada = await armatureAs(admin.token);
    try {
      const own = async () => {
        const { webhooks } = await answered<{ webhooks: Endpoint[] }>(armature.get("webhooks"), 200, "the webhooks");
        return webhooks.find((each) => each.partner?.clientId === clientId);
      };
      const hook = await own();
      expect(hook, "Stator's own webhook in Armature").toMatchObject({ name: "Stator (connected app)", url: WEBHOOK_URL });
      expect(hook!.topics).toEqual([...connection.webhookTopics].sort());

      await section.locator('[data-action="test-armature-webhook"]').click();
      await expect(section.locator("[data-armature-webhook-said]")).toHaveText("Armature reached Stator, which answered 204.");

      // A change made in Armature reaches Stator through that webhook alone:
      // the seed's own endpoint signs with a secret Stator no longer holds.
      const startedAt = Date.now();
      const { issue } = await answered<{ issue: { key: string } }>(
        ada.post("issues", { data: { projectKey: seed.project.key, summary: "Announce the one-step connect" } }),
        201,
        "an issue to change",
      );
      await answered(ada.patch(`issues/${issue.key}`, { data: { summary: "Announce the one-step connect, in the newsletter" } }), 200, "rename the issue");
      await expect
        .poll(async () => {
          const { deliveries } = await answered<{ deliveries: Array<{ topic: string; status: number | null }> }>(
            armature.get(`webhooks/${hook!.id}/deliveries`),
            200,
            "the deliveries",
          );
          return deliveries.filter((each) => each.topic === "issue.updated").map((each) => each.status);
        }, WORKER_DELIVERS)
        .toContain(204);
      // The issue's creation may be delivered after its change, so the last
      // delivery taken is either, and newer than the change.
      await expect
        .poll(async () => {
          const last = (await api.GET("/armature/connection")).data?.connection?.lastDelivery;
          return Boolean(last && connection.webhookTopics.includes(last.topic) && Date.parse(last.receivedAt) >= startedAt - CLOCK_SLACK_MS);
        }, WORKER_DELIVERS)
        .toBe(true);

      // Disconnecting removes it from Armature again.
      page.once("dialog", (dialog) => dialog.accept());
      await page.locator('[data-action="disconnect-armature"]').click();
      await expect(page.getByText("Armature is disconnected.")).toBeVisible();
      await expect.poll(own).toBeUndefined();
    } finally {
      await armature.dispose();
      await ada.dispose();
    }
  });
});
