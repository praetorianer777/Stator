import type { APIRequestContext, Page } from "@playwright/test";
import { must } from "../fixtures/api";
import { expect } from "../fixtures/auth";
import { teamTest as test } from "../fixtures/team";
import { expectAccessible } from "../fixtures/shell";
import { armatureURL } from "../fixtures/stack";

// Connecting one's account with one click (#428), through the stub's consent
// page. The stub offers connected apps when reached by the host below, which
// the api's backchannel maps to it as it maps the stub's own address; a host
// of its own keeps what the api learned of it from every other spec.
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
];

function connectedApp(): { base: string; host: string } {
  const url = new URL(armatureURL());
  url.hostname = "connected-app.localhost";
  return { base: url.origin, host: url.host };
}

async function stub(request: APIRequestContext, method: string, path: string, data?: unknown) {
  const answer = await request.fetch(`${armatureURL()}${path}`, { method, data });
  expect(answer.status(), `${method} ${path}`).toBeLessThan(300);
  return answer.status() === 204 ? null : answer.json();
}

interface Grant {
  person: string;
  ended: boolean;
}

async function grantsOf(request: APIRequestContext, tenant: string, person: string): Promise<Grant[]> {
  const body = (await stub(request, "GET", `/_stub-oauth/${tenant}/grants`)) as { grants: Grant[] };
  return body.grants.filter((g) => g.person === person);
}

/** Allows Stator on the stub's consent page as person of tenant. */
async function allow(page: Page, tenant: string, person: string) {
  await expect(page).toHaveURL(new RegExp(`^${connectedApp().base}/oauth/authorize\\?`));
  await page.getByLabel("Armature organization").fill(tenant);
  await page.getByLabel("Your name in Armature").fill(person);
  await page.getByRole("button", { name: "Allow" }).click();
}

test.describe("connecting Armature with one click", { tag: ["@auth"] }, () => {
  test.beforeEach(async ({ api, request }) => {
    await stub(request, "PUT", `/_stub-meta/${connectedApp().host}`, { capabilities: CAPABILITIES });
    await stub(request, "PUT", `/_stub-oauth/hosts/${connectedApp().host}`, { registration: "approve" });
    await api.DELETE("/armature/connection");
    must(await api.PUT("/armature/connection", { body: { baseUrl: connectedApp().base } }));
  });

  test.afterEach(async ({ api, request }) => {
    await api.DELETE("/armature/connection");
    await stub(request, "DELETE", `/_stub-meta/${connectedApp().host}`);
  });

  test("an administrator connects with one click, allowing Stator on the way, and disconnects", async ({ page, request, team }) => {
    const tenant = team.label;
    await page.goto("/settings/profile");
    const section = page.locator("[data-armature-account]");
    await expect(section.locator('[data-action="connect-armature-app"]')).toHaveText("Connect Armature");
    await expect(section.getByLabel("Armature token", { exact: true })).toHaveCount(0);
    await expectAccessible(page);

    await section.locator('[data-action="connect-armature-app"]').click();
    await allow(page, tenant, "admin");

    await expect(page).toHaveURL(/\/settings\/profile\?armature=connected#armature$/);
    await expect(section.locator("[data-armature-user]")).toHaveText(`Connected as Admin (admin@${tenant}.armature.test).`);
    await expect(section.getByText("Connected through Armature's connected apps.")).toBeVisible();
    await expect(page.getByRole("status").filter({ hasText: "Your Armature account is connected." })).toBeVisible();
    await expect(section.locator("[data-armature-via]")).toHaveAttribute("data-armature-via", "app");
    await expectAccessible(page);
    expect((await grantsOf(request, tenant, "admin")).filter((g) => !g.ended)).toHaveLength(1);

    page.once("dialog", (dialog) => dialog.accept());
    await section.locator('[data-action="disconnect-armature-account"]').click();
    await expect(section.getByText("Your Armature account is disconnected.")).toBeVisible();
    await expect(section.locator('[data-action="connect-armature-app"]')).toBeVisible();
    await expect.poll(async () => (await grantsOf(request, tenant, "admin")).every((g) => g.ended)).toBe(true);
  });

  test("a hint to connect goes to Armature and back to where it was, and a cancel says so", async ({ page, team }) => {
    const tenant = team.label;
    await page.goto("/settings/armature");
    const hint = page.locator("[data-armature-members]").getByRole("link", { name: "Connect your own account" });
    await expect(hint).toHaveAttribute("data-one-click", "");

    await hint.click();
    await expect(page).toHaveURL(new RegExp(`^${connectedApp().base}/oauth/authorize\\?`));
    await page.getByRole("button", { name: "Cancel" }).click();
    await expect(page).toHaveURL(/\/settings\/profile\?/);
    await expect(page.locator('[data-armature-outcome="denied"]')).toContainText("You did not allow Stator in Armature");
    await expectAccessible(page);

    await page.goto("/settings/armature");
    await page.locator("[data-armature-members]").getByRole("link", { name: "Connect your own account" }).click();
    await allow(page, tenant, "admin");
    await expect(page).toHaveURL(/\/settings\/armature$/);
    await expect(page.locator("[data-armature-returned]")).toHaveText(/Your Armature account is connected, and here is the page you came from\./);
    await expect(page.locator("[data-armature-app]")).toHaveAttribute("data-armature-app", "active");
  });

  test("a member waits for an Armature administrator to allow a pending app", async ({ pageAs, team }) => {
    const bob = await pageAs("bob");
    await bob.goto("/settings/profile");
    await bob.locator('[data-action="connect-armature-app"]').click();
    await expect(bob).toHaveURL(new RegExp(`^${connectedApp().base}/oauth/authorize\\?`));
    await bob.getByLabel("Armature organization").fill(team.label);
    await bob.getByLabel("Your name in Armature").fill("bob");
    await bob.getByRole("button", { name: "Allow" }).click();
    await expect(bob.getByRole("alert")).toHaveText("Stator is not connected to Armature yet. An Armature administrator connects it first.");

    await bob.goto("/settings/profile");
    await expect(bob.locator("[data-armature-app-pending]")).toContainText("An Armature administrator has to allow Stator first.");
  });
});
