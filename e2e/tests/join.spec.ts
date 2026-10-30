import type { Browser, BrowserContext, Page } from "@playwright/test";
import { must, type StatorApi } from "../fixtures/api";
import { ME_PATH, authTest as test, expect, startSSO, submitKeycloak } from "../fixtures/auth";
import { ONE_LOOK, openUntil } from "../fixtures/replica";
import { expectAccessible, openNavigation, settingsSidebar, toast } from "../fixtures/shell";
import { uniqueName } from "../fixtures/seed";
import { WEB_URL } from "../fixtures/stack";

const ANONYMOUS = { cookies: [], origins: [] };
const MEMBERS_PATH = "/settings/access/members";
const GROUPS_PATH = "/settings/access/groups";

// A realm user the seed has not let into the demo organisation, from deploy/keycloak/realm.json.
const CAROL = { username: "carol", password: "carol password", email: "carol@suite.test", name: "Carol Checker" };
const BOB = { username: "bob", password: "bob password", email: "bob@suite.test" };
// A group of bob's at the provider that alice is not in. He is a member here
// already, so a group granting him Member adds a grant he holds anyway, which
// keeps every other spec that signs him in unaffected.
const BOB_GROUP = "developers";

const member = (page: Page, email: string) => page.locator(`[data-member="${email}"]`);
const groupCard = (page: Page, name: string) => page.locator(`[data-group="${name}"]`);

/** Deletes the group of that name, with its grants; one that is gone already is fine. */
async function dropGroup(api: StatorApi, name: string) {
  const { groups } = must(await api.GET("/groups", { params: { query: { q: name, limit: 50 } } }));
  for (const group of groups.filter((each) => each.name === name)) {
    await api.DELETE("/groups/{groupID}", { params: { path: { groupID: group.id } } });
  }
}

/** Signs somebody in through Keycloak in a browser of their own, and says where they landed. */
async function signInFresh(browser: Browser, username: string, password: string): Promise<{ context: BrowserContext; page: Page }> {
  const context = await browser.newContext({ baseURL: WEB_URL, storageState: ANONYMOUS });
  const page = await context.newPage();
  await startSSO(page);
  await submitKeycloak(page, username, password);
  return { context, page };
}

/** The badge beside access in the settings sidebar, read on a settings page, with a phone's drawer closed again. */
async function expectWaiting(page: Page, count: number) {
  const opened = await openNavigation(page);
  const badge = settingsSidebar(page).locator('[data-nav="access"] [data-count="join-requests"]');
  if (count === 0) await expect(badge).toHaveCount(0);
  else await expect(badge).toContainText(`${count} waiting`);
  if (opened) await page.keyboard.press("Escape");
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
    await dropGroup(api, BOB_GROUP);
  });

  test("a Keycloak user nobody has approved waits, and turning her away takes the count down", async ({ browser, page }) => {
    const carol = await signInFresh(browser, CAROL.username, CAROL.password);
    await expect(carol.page).toHaveURL(/\/login\?sso=not_a_member/);
    await expect(carol.page.getByRole("status")).toContainText(/waiting for an administrator/i);
    expect((await carol.page.request.get(ME_PATH)).status()).toBe(401);
    await expectAccessible(carol.page);
    await carol.context.close();

    await page.goto(MEMBERS_PATH);
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

    await page.goto(MEMBERS_PATH);
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

    await openUntil(page, MEMBERS_PATH, async () => {
      await expect(member(page, CAROL.email).locator("[data-member-role]")).toHaveText("Member", ONE_LOOK);
    });
    await expect(member(page, CAROL.email).locator('[data-member-holds="member"]')).toContainText("through Everyone");
    await member(page, CAROL.email)
      .getByRole("button", { name: `Remove ${CAROL.name}` })
      .click();
    await page.locator("[data-confirm]").locator('[data-action="confirm"]').click();
    await expect(page.getByText(`${CAROL.name} was removed.`)).toBeVisible();
    await expect(member(page, CAROL.email)).toHaveCount(0);

    // Her open session stops reaching the organization at once.
    expect((await (await carol.page.request.get(ME_PATH)).json()).organization ?? null).toBeNull();
    await carol.context.close();
  });

  test("a group the provider fills holds its grants for whoever signs in through it, and takes them along when it goes", async ({ browser, page }) => {
    await page.goto(GROUPS_PATH);
    await page.getByRole("textbox", { name: "New group" }).fill(BOB_GROUP);
    await page.getByRole("textbox", { name: "Provider claim" }).fill(BOB_GROUP);
    await page.locator('[data-action="create-group"]').click();
    await expect(toast(page, `${BOB_GROUP} was made.`)).toBeVisible();
    await expect(groupCard(page, BOB_GROUP)).toContainText("From the identity provider");

    await page.goto("/settings/access/roles");
    const form = page.locator("[data-grant-form]");
    await form.getByRole("combobox").first().fill(BOB_GROUP);
    await form.locator(`[data-subject-option="${BOB_GROUP}"]`).click();
    await form.locator("[data-grant-role-choice]").selectOption("member");
    await form.locator('[data-action="grant-role"]').click();
    await expect(toast(page, `${BOB_GROUP} holds Member now.`)).toBeVisible();

    const bob = await signInFresh(browser, BOB.username, BOB.password);
    await expect(bob.page).not.toHaveURL(/\/login/);
    await bob.context.close();
    await openUntil(page, MEMBERS_PATH, async () => {
      await expect(
        member(page, BOB.email)
          .locator('[data-member-holds="member"]')
          .filter({ hasText: `through ${BOB_GROUP}` }),
      ).toBeVisible(ONE_LOOK);
    });
    await expect(member(page, BOB.email).locator("[data-member-role]")).toHaveText("Member");

    await page.goto(GROUPS_PATH);
    await groupCard(page, BOB_GROUP).locator('[data-action="delete-group"]').click();
    await page.locator("[data-confirm]").locator('[data-action="confirm"]').click();
    await expect(toast(page, `${BOB_GROUP} was deleted.`)).toBeVisible();
    await expect(groupCard(page, BOB_GROUP)).toHaveCount(0);

    await openUntil(page, MEMBERS_PATH, async () => {
      await expect(member(page, BOB.email).locator("[data-member-role]")).toHaveText("Member", ONE_LOOK);
      await expect(
        member(page, BOB.email)
          .locator('[data-member-holds="member"]')
          .filter({ hasText: `through ${BOB_GROUP}` }),
      ).toHaveCount(0, ONE_LOOK);
    });
  });
});

test.describe("groups", { tag: "@auth" }, () => {
  test("an administrator makes a group kept by hand, fills it and deletes it", async ({ page, api }, testInfo) => {
    const group = uniqueName(testInfo, "group").replaceAll(" ", "-");
    const alice = must(await api.GET("/auth/me")).user.name;
    try {
      await page.goto(GROUPS_PATH);
      await expect(page.locator('[data-group-builtin="everyone"]')).toBeVisible();
      await page.getByRole("textbox", { name: "New group" }).fill(group);
      await page.locator('[data-action="create-group"]').click();
      await expect(toast(page, `${group} was made.`)).toBeVisible();
      const card = groupCard(page, group);
      await card.getByRole("combobox").fill(alice);
      await card.locator(`[data-subject-option="${alice}"]`).click();
      await expect(toast(page, `${alice} is in ${group} now.`)).toBeVisible();
      await expect(card.locator(`[data-group-member="${alice}"]`)).toBeVisible();

      await page.reload();
      await expect(groupCard(page, group).locator(`[data-group-member="${alice}"]`)).toBeVisible();
      await expectAccessible(page);

      await groupCard(page, group).locator('[data-action="delete-group"]').click();
      await page.locator("[data-confirm]").locator('[data-action="confirm"]').click();
      await expect(toast(page, `${group} was deleted.`)).toBeVisible();
      await expect(groupCard(page, group)).toHaveCount(0);
    } finally {
      await dropGroup(api, group);
    }
  });
});
