import type { Browser, BrowserContext, Page } from "@playwright/test";
import { must, type StatorApi } from "../fixtures/api";
import { ME_PATH, authTest as test, expect, startSSO, submitKeycloak } from "../fixtures/auth";
import { expectAccessible } from "../fixtures/shell";
import { uniqueName } from "../fixtures/seed";
import { WEB_URL } from "../fixtures/stack";

const ANONYMOUS = { cookies: [], origins: [] };
const SSO_PATH = "/settings/sso";

// A realm user the seed has not let into the demo organisation, from deploy/keycloak/realm.json.
const CAROL = { username: "carol", password: "carol password", email: "carol@stator.test", name: "Carol Candidate" };
const BOB = { username: "bob", password: "bob password", email: "bob@stator.test" };
// bob's group at the provider. He is a member here already, so mapping it to
// member changes who decides his role and never the role itself, which keeps
// every other spec that signs him in unaffected.
const BOB_GROUP = "marketing";

// Reads may come from a replica, and alice sees somebody else's sign-in only
// once the replica has it, so her page reloads until it has.
const REPLICA_CATCH_UP_MS = 10_000;
const RECHECK_MS = 1_000;
async function afterReplication(page: Page, check: () => Promise<void>) {
  await expect(async () => {
    await page.reload();
    await check();
  }).toPass({ timeout: REPLICA_CATCH_UP_MS, intervals: [RECHECK_MS] });
}

const accountItem = (page: Page, action: string) => page.locator(`[role="menu"] [data-action="${action}"]`);
const member = (page: Page, email: string) => page.locator(`[data-member="${email}"]`);
const groupRole = (page: Page, group: string) => page.locator(`[data-group-role="${group}"]`);

/** Signs somebody in through Keycloak in a browser of their own, and says where they landed. */
async function signInFresh(browser: Browser, username: string, password: string): Promise<{ context: BrowserContext; page: Page }> {
  const context = await browser.newContext({ baseURL: WEB_URL, storageState: ANONYMOUS });
  const page = await context.newPage();
  await startSSO(page);
  await submitKeycloak(page, username, password);
  return { context, page };
}

/** The badge beside single sign-on in the account menu, read with the menu open and closed again. */
async function expectWaiting(page: Page, count: number) {
  await page.locator('[data-action="account"]').click();
  const badge = accountItem(page, "sso-settings").locator("[data-join-badge]");
  if (count === 0) await expect(badge).toHaveCount(0);
  else await expect(badge).toContainText(`${count} waiting`);
  await page.keyboard.press("Escape");
}

/** Leaves carol as the demo organisation had her: no request, no membership. */
async function forgetCarol(api: StatorApi) {
  const waiting = must(await api.GET("/users/requests")).requests.find((each) => each.email === CAROL.email);
  if (waiting) await api.DELETE("/users/requests/{userID}", { params: { path: { userID: waiting.userId } } });
  const joined = must(await api.GET("/users")).members.find((each) => each.email === CAROL.email);
  if (joined) expect((await api.DELETE("/users/{userID}", { params: { path: { userID: joined.userId } } })).response.status).toBe(204);
}

// Carol and bob are shared by the whole run, so these walk one at a time and
// at one width: two of them at once would answer each other's requests.
test.describe("letting people in", { tag: ["@auth", "@desktop"] }, () => {
  test.describe.configure({ mode: "serial" });

  test.afterEach(async ({ api }) => {
    await forgetCarol(api);
    const mapped = must(await api.GET("/oidc-provider/group-roles")).groupRoles.find((each) => each.group === BOB_GROUP);
    if (mapped) await api.DELETE("/oidc-provider/group-roles/{groupRoleID}", { params: { path: { groupRoleID: mapped.id } } });
  });

  test("a Keycloak user nobody has approved waits, and turning her away takes the count down", async ({ browser, page }) => {
    const carol = await signInFresh(browser, CAROL.username, CAROL.password);
    await expect(carol.page).toHaveURL(/\/login\?sso=not_a_member/);
    await expect(carol.page.getByRole("status")).toContainText(/waiting for an administrator/i);
    expect((await carol.page.request.get(ME_PATH)).status()).toBe(401);
    await expectAccessible(carol.page);
    await carol.context.close();

    await page.goto(SSO_PATH);
    const request = page.locator(`[data-join-request="${CAROL.email}"]`);
    await expect(request).toBeVisible();
    await expectWaiting(page, 1);

    await request.getByRole("button", { name: "Turn away" }).click();
    await expect(page.getByText(`${CAROL.name} was turned away.`)).toBeVisible();
    await expect(request).toHaveCount(0);
    await expectWaiting(page, 0);
  });

  test("alice lets carol in with a click, carol lands in the app, and alice removes her again", async ({ browser, page }) => {
    const first = await signInFresh(browser, CAROL.username, CAROL.password);
    await expect(first.page).toHaveURL(/\/login\?sso=not_a_member/);
    await first.context.close();

    await page.goto(SSO_PATH);
    await expectWaiting(page, 1);
    await page.locator(`[data-join-request="${CAROL.email}"]`).getByRole("button", { name: "Let in as member" }).click();
    await expect(page.getByText(`${CAROL.name} may sign in now.`)).toBeVisible();
    await expectWaiting(page, 0);

    const carol = await signInFresh(browser, CAROL.username, CAROL.password);
    await expect(carol.page).not.toHaveURL(/\/login/);
    await expect(carol.page.locator("main#main")).toBeVisible();
    const me = await carol.page.request.get(ME_PATH);
    expect(me.status()).toBe(200);
    expect((await me.json()).organization.role).toBe("member");

    await afterReplication(page, async () => {
      await expect(member(page, CAROL.email).locator("[data-member-role]")).toHaveText("Member", { timeout: RECHECK_MS });
    });
    await expect(member(page, CAROL.email).locator("[data-role-source]")).toHaveCount(0);
    await member(page, CAROL.email)
      .getByRole("button", { name: `Remove ${CAROL.name}` })
      .click();
    await page.locator('[data-action="confirm-remove-member"]').click();
    await expect(page.getByText(`${CAROL.name} was removed.`)).toBeVisible();
    await expect(member(page, CAROL.email)).toHaveCount(0);

    // Her open session stops reaching the organization at once.
    expect((await (await carol.page.request.get(ME_PATH)).json()).organization ?? null).toBeNull();
    await carol.context.close();
  });

  test("a group mapped to a role marks the roles it decides as coming from the identity provider", async ({ browser, page }) => {
    await page.goto(SSO_PATH);
    const card = page.locator("[data-group-roles]");
    await card.getByLabel("Group", { exact: true }).fill(BOB_GROUP);
    await card.getByLabel("Role", { exact: true }).selectOption("member");
    await card.locator('[data-action="map-group"]').click();
    await expect(groupRole(page, BOB_GROUP)).toContainText("Member");

    const bob = await signInFresh(browser, BOB.username, BOB.password);
    await expect(bob.page).not.toHaveURL(/\/login/);
    await bob.context.close();
    await afterReplication(page, async () => {
      await expect(member(page, BOB.email).locator("[data-role-source]")).toHaveText("From identity provider", { timeout: RECHECK_MS });
    });
    await expect(member(page, BOB.email).locator("[data-member-role]")).toHaveText("Member");

    await groupRole(page, BOB_GROUP)
      .getByRole("button", { name: `Unmap ${BOB_GROUP}` })
      .click();
    await expect(groupRole(page, BOB_GROUP)).toHaveCount(0);

    // His next sign-in hands the role back to the administrators, as it was.
    const again = await signInFresh(browser, BOB.username, BOB.password);
    await again.context.close();
    await afterReplication(page, async () => {
      await expect(member(page, BOB.email).locator("[data-member-role]")).toHaveText("Member", { timeout: RECHECK_MS });
      await expect(member(page, BOB.email).locator("[data-role-source]")).toHaveCount(0, { timeout: RECHECK_MS });
    });
  });
});

test.describe("mapping groups to roles", { tag: "@auth" }, () => {
  test("an administrator maps a group to admin and unmaps it again", async ({ page, api }, testInfo) => {
    // A group nobody at the provider is in, so the mapping changes nobody's role.
    const group = uniqueName(testInfo, "group").replaceAll(" ", "-");
    try {
      await page.goto(SSO_PATH);
      const card = page.locator("[data-group-roles]");
      await expect(card.getByRole("heading", { name: "Roles from groups" })).toBeVisible();
      await card.getByLabel("Group", { exact: true }).fill(group);
      await card.getByLabel("Role", { exact: true }).selectOption("admin");
      await card.locator('[data-action="map-group"]').click();
      await expect(card.getByRole("status")).toHaveText(`${group} now grants Admin.`);
      await expect(groupRole(page, group)).toContainText("Admin");
      await expect(card.getByLabel("Group", { exact: true })).toHaveValue("");

      await page.reload();
      await expect(groupRole(page, group)).toContainText("Admin");
      await expectAccessible(page);

      await groupRole(page, group)
        .getByRole("button", { name: `Unmap ${group}` })
        .click();
      await expect(card.getByRole("status")).toHaveText(`${group} no longer grants a role.`);
      await expect(groupRole(page, group)).toHaveCount(0);
    } finally {
      const left = must(await api.GET("/oidc-provider/group-roles")).groupRoles.find((each) => each.group === group);
      if (left) await api.DELETE("/oidc-provider/group-roles/{groupRoleID}", { params: { path: { groupRoleID: left.id } } });
    }
  });

  test("a blank group is refused with a sentence beside the field", async ({ page }) => {
    await page.goto(SSO_PATH);
    const card = page.locator("[data-group-roles]");
    await card.locator('[data-action="map-group"]').click();
    await expect(card.getByText("Enter the group's name as the provider sends it in the groups claim.")).toBeVisible();
  });
});
