import type { Page } from "@playwright/test";
import { expect } from "../fixtures/auth";
import { teamTest as test } from "../fixtures/team";
import { armatureURL } from "../fixtures/stack";
import { readSeed, realArmatureURL } from "./armature";

// Connecting the real Armature as an administrator and a member would: the
// address and slug Armature answers to, and tokens Armature made.

const REFUSED = "Armature did not accept this token. Make a new one under Tokens in Armature and paste it here.";

async function paste(page: Page, token: string): Promise<void> {
  await page.getByLabel("Armature token", { exact: true }).fill(token);
  await page.locator('[data-action="connect-armature"]').click();
}

test.describe("connecting a real Armature", { tag: ["@auth"] }, () => {
  test.beforeEach(async ({ api }) => {
    await api.DELETE("/armature/connection");
  });

  test.afterEach(async ({ api }) => {
    await api.DELETE("/armature/connection");
  });

  test("an administrator connects Armature, and each member stores the token Armature made them", async ({ page, pageAs }) => {
    const seed = readSeed();

    await page.goto("/settings/armature");
    await page.getByLabel("Armature address", { exact: true }).fill(seed.url);
    await page.getByLabel("Armature organization", { exact: true }).fill(seed.orgSlug);
    await page.getByLabel("Webhook secret", { exact: true }).fill(seed.webhookSecret);
    await page.locator('[data-action="save-armature"]').click();
    await expect(page.getByText("Saved.", { exact: true })).toBeVisible();
    await expect(page.locator("body")).not.toContainText(seed.webhookSecret);

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

    // The settings name the address Armature's webhook is to post to.
    await page.goto("/settings/armature");
    await expect(page.getByLabel("Webhook address", { exact: true })).toHaveValue(/\/api\/v1\/armature\/webhook$/);
  });

  test("Stator reads what the real Armature offers, which the stub reports too", async ({ api, page, request }) => {
    const seed = readSeed();
    const meta = await request.get(`${realArmatureURL()}/api/v1/meta`);
    expect(meta.status()).toBe(200);
    const real = (await meta.json()) as { product: string; capabilities: string[] };
    expect(real.product).toBe("armature");

    const saved = await api.PUT("/armature/connection", { body: { baseUrl: seed.url, orgSlug: seed.orgSlug } });
    expect(saved.response.status).toBe(200);
    const partner = saved.data!.connection.partner;
    expect(partner.status).toBe("ok");
    expect(partner.meta).toBe(true);
    expect(partner.capabilities).toEqual([...real.capabilities].sort());
    // Everything Stator relies on is there, so nothing is turned off.
    expect(partner.missing).toEqual([]);
    // The stub the other suites run against answers as this Armature does.
    const stub = (await (await request.get(`${armatureURL()}/api/v1/meta`)).json()) as { capabilities: string[] };
    expect([...stub.capabilities].sort()).toEqual([...real.capabilities].sort());

    await page.goto("/settings/armature");
    await expect(page.locator("[data-armature-capabilities]")).toHaveAttribute("data-armature-capabilities", "all");
    await expect(page.getByText("Armature offers everything Stator uses.")).toBeVisible();
  });

  test("a slug that is not the installation's turns every token away", async ({ api, page }) => {
    const seed = readSeed();
    expect((await api.PUT("/armature/connection", { body: { baseUrl: seed.url, orgSlug: `${seed.orgSlug}-elsewhere` } })).response.status).toBe(200);
    await page.goto("/settings/profile");
    await paste(page, seed.people.member.token);
    await expect(page.locator("[data-armature-account]")).toContainText("another Armature organization");
  });
});
