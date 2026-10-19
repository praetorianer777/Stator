import type { APIRequestContext, Page } from "@playwright/test";
import { expect } from "../fixtures/auth";
import { teamTest as test } from "../fixtures/team";
import { armatureURL } from "../fixtures/stack";
import { answered, armatureSignedIn, passwordOf, readSeed, realArmatureURL, type ArmaturePerson } from "./armature";

// Connecting the real Armature as an administrator and a member would: the
// address and slug Armature answers to, Armature's own sign-in and consent
// page, and tokens Armature made.

const REFUSED = "Armature did not accept this token. Make a new one under Tokens in Armature and paste it here.";
// What the real Armature offers beyond the stub's default: connected apps and
// the partner webhook, which the stub offers by host.
const BEYOND_THE_STUB = ["oauth.partner-tokens@1", "webhooks.partner@1"];

/** Pastes a token, opening the form behind Connect Armature where Armature offers connected apps. */
async function paste(page: Page, token: string): Promise<void> {
  const field = page.getByLabel("Armature token", { exact: true });
  if (!(await field.isVisible())) await page.locator('[data-action="use-armature-token"]').click();
  await field.fill(token);
  await page.locator('[data-action="connect-armature"]').click();
}

/** Signs in on Armature's own sign-in page, where the consent page sends somebody not signed in. */
async function signInToArmature(page: Page, person: ArmaturePerson): Promise<void> {
  await expect(page).toHaveURL(new RegExp(`^${realArmatureURL()}/login\\?`));
  await page.getByLabel("Email", { exact: true }).fill(person.email);
  await page.getByLabel("Password", { exact: true }).fill(passwordOf(person.email));
  await page.getByRole("button", { name: "Sign in", exact: true }).click();
  await expect(page).toHaveURL(new RegExp(`^${realArmatureURL()}/oauth/authorize\\?`));
}

/** The grants of Stator's app in a person's token list in Armature. */
async function grantsOf(armature: APIRequestContext, clientId: string): Promise<unknown[]> {
  const { tokens } = await answered<{ tokens: Array<{ app?: { clientId: string } | null }> }>(armature.get("tokens"), 200, "the token list");
  return tokens.filter((each) => each.app?.clientId === clientId);
}

test.describe("connecting a real Armature", { tag: ["@auth"] }, () => {
  test.beforeEach(async ({ api }) => {
    await api.DELETE("/armature/connection");
  });

  test.afterEach(async ({ api }) => {
    await api.DELETE("/armature/connection");
  });

  test("an administrator allows Stator on Armature's consent page, a member connects with one click, and Disconnect ends it", async ({ api, page, pageAs }) => {
    const seed = readSeed();
    const { admin, member } = seed.people;

    await page.goto("/settings/armature");
    await page.getByLabel("Armature address", { exact: true }).fill(seed.url);
    await page.getByLabel("Webhook secret", { exact: true }).fill(seed.webhookSecret);
    await page.locator('[data-action="save-armature"]').click();
    await expect(page.getByText("Saved.", { exact: true })).toBeVisible();
    await expect(page.locator("body")).not.toContainText(seed.webhookSecret);
    // The settings name the address Armature's webhook is to post to.
    await expect(page.getByLabel("Webhook address", { exact: true })).toHaveValue(/\/api\/v1\/armature\/webhook$/);

    // The first connect registers Stator, which stays pending until an
    // Armature administrator allows it on the consent page: alice connects as Ada.
    await page.goto("/settings/profile");
    const section = page.locator("[data-armature-account]");
    await expect(section.getByLabel("Armature token", { exact: true })).toHaveCount(0);
    await section.locator('[data-action="connect-armature-app"]').click();
    await signInToArmature(page, admin);
    const approval = page.locator('[data-consent="Stator"]');
    await expect(approval).toHaveAttribute("data-consent-pending", "true");
    await expect(approval.locator("[data-consent-approve]")).toContainText("asks to be connected to Armature");
    await expect(approval).toContainText(`wants to act as ${admin.name} in Armature`);
    await expect(approval.locator('[data-consent-scope="issue.write"]')).toBeVisible();
    await page.locator('[data-action="consent-allow"]').click();
    await expect(page).toHaveURL(/\/settings\/profile\?armature=connected#armature$/);
    await expect(section.locator("[data-armature-user]")).toHaveText(`Connected as ${admin.name} (${admin.email}).`);
    await expect(section.locator("[data-armature-via]")).toHaveAttribute("data-armature-via", "app");
    const app = (await api.GET("/armature/connection")).data!.connection!.app;
    expect(app.status).toBe("active");
    const clientId = app.clientId!;

    // Bob, a member, connects as Mia and is asked for nothing but her own consent.
    const bob = await pageAs("bob");
    await bob.goto("/settings/profile");
    const bobs = bob.locator("[data-armature-account]");
    await expect(bobs.locator("[data-armature-app-pending]")).toHaveCount(0);
    await bobs.locator('[data-action="connect-armature-app"]').click();
    await signInToArmature(bob, member);
    const consent = bob.locator('[data-consent="Stator"]');
    await expect(consent).toHaveAttribute("data-consent-pending", "false");
    await expect(consent).toContainText(`wants to act as ${member.name} in Armature`);
    await bob.locator('[data-action="consent-allow"]').click();
    await expect(bob).toHaveURL(/\/settings\/profile\?armature=connected#armature$/);
    await expect(bobs.locator("[data-armature-user]")).toHaveText(`Connected as ${member.name} (${member.email}).`);
    await expect(bob.getByRole("status").filter({ hasText: "Your Armature account is connected." })).toBeVisible();

    // Stator reads Armature as Mia through her grant.
    const issuePath = `/api/v1/armature/issues/${seed.issues.guide.key}`;
    const read = await answered<{ status: string; issue: { key: string; summary: string } | null }>(bob.request.get(issuePath), 200, "the guide as Mia");
    expect(read.status).toBe("ok");
    expect(read.issue).toMatchObject({ key: seed.issues.guide.key, summary: seed.issues.guide.summary });

    const mia = await armatureSignedIn(member.email);
    try {
      expect(await grantsOf(mia, clientId)).toHaveLength(1);
      bob.once("dialog", (dialog) => dialog.accept());
      await bobs.locator('[data-action="disconnect-armature-account"]').click();
      await expect(bobs.getByText("Your Armature account is disconnected.")).toBeVisible();
      await expect(bobs.locator('[data-action="connect-armature-app"]')).toBeVisible();
      // Stator revokes the grant at Armature, so it is gone from her token list there too.
      await expect.poll(async () => grantsOf(mia, clientId)).toHaveLength(0);
    } finally {
      await mia.dispose();
    }
    const after = await answered<{ status: string }>(bob.request.get(issuePath), 200, "the guide once Bob disconnected");
    expect(after.status).toBe("not_connected");
  });

  test("a member stores the token Armature made them instead, and a wrong one is refused", async ({ api, page, pageAs }) => {
    const seed = readSeed();
    expect((await api.PUT("/armature/connection", { body: { baseUrl: seed.url } })).response.status).toBe(200);

    await page.goto("/settings/profile");
    const section = page.locator("[data-armature-account]");
    await paste(page, `${seed.people.member.token.slice(0, -4)}xxxx`);
    await expect(section.getByText(REFUSED)).toBeVisible();
    await paste(page, seed.people.member.token);
    await expect(section.locator("[data-armature-user]")).toHaveText(`Connected as ${seed.people.member.name} (${seed.people.member.email}).`);
    await expect(page.locator("body")).not.toContainText(seed.people.member.token);
    await section.locator('[data-action="check-armature"]').click();
    await expect(section.getByText("Armature accepts your token.")).toBeVisible();

    // A member without access to any project is still somebody in Armature.
    const bob = await pageAs("bob");
    await bob.goto("/settings/profile");
    await paste(bob, seed.people.outsider.token);
    await expect(bob.locator("[data-armature-account] [data-armature-user]")).toHaveText(
      `Connected as ${seed.people.outsider.name} (${seed.people.outsider.email}).`,
    );
  });

  test("Stator reads what the real Armature offers, which the stub reports too", async ({ api, page, request }) => {
    const seed = readSeed();
    const meta = await request.get(`${realArmatureURL()}/api/v1/meta`);
    expect(meta.status()).toBe(200);
    const real = (await meta.json()) as { product: string; capabilities: string[] };
    expect(real.product).toBe("armature");

    const saved = await api.PUT("/armature/connection", { body: { baseUrl: seed.url } });
    expect(saved.response.status).toBe(200);
    const partner = saved.data!.connection.partner;
    expect(partner.status).toBe("ok");
    expect(partner.meta).toBe(true);
    expect(partner.capabilities).toEqual([...real.capabilities].sort());
    // Everything Stator relies on is there, so nothing is turned off.
    expect(partner.missing).toEqual([]);
    // The stub the other suites run against answers as this Armature does.
    const stub = (await (await request.get(`${armatureURL()}/api/v1/meta`)).json()) as { capabilities: string[] };
    expect([...stub.capabilities, ...BEYOND_THE_STUB].sort()).toEqual([...real.capabilities].sort());

    await page.goto("/settings/armature");
    await expect(page.locator("[data-armature-capabilities]")).toHaveAttribute("data-armature-capabilities", "all");
    await expect(page.getByText("Armature offers everything Stator uses.")).toBeVisible();
  });

  test("the first token that checks out teaches Stator the installation, by the id Armature names", async ({ api, page }) => {
    const seed = readSeed();
    expect((await api.PUT("/armature/connection", { body: { baseUrl: seed.url } })).response.status).toBe(200);
    await page.goto("/settings/profile");
    await paste(page, seed.people.member.token);
    await expect(page.locator("[data-armature-account]")).not.toContainText("another Armature installation");
    await expect.poll(async () => (await api.GET("/armature/connection")).data?.connection?.installationId).toBe(seed.installationId);
    await page.goto("/settings/armature");
    await expect(page.getByText(/confirmed its installation through a member's token/)).toBeVisible();
  });
});
