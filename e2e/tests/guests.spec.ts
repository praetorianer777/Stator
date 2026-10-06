import type { Browser, BrowserContext, Page } from "@playwright/test";
import { must } from "../fixtures/api";
import { ME_PATH, expect, startSSO, submitKeycloak } from "../fixtures/auth";
import { orgTest as test } from "../fixtures/org";
import { openShowing } from "../fixtures/replica";
import { expectAccessible } from "../fixtures/shell";
import { createPage, createSpace, deleteSpace, uniqueKey } from "../fixtures/spaces";
import { WEB_URL } from "../fixtures/stack";

const ANONYMOUS = { cookies: [], origins: [] };
// A realm user no throwaway organisation lets in, from deploy/keycloak/realm.json.
const CAROL = { username: "carol", password: "carol password", email: "carol@stator.test" };
const BOB_EMAIL = "bob@stator.test";

const guestsCard = (page: Page, key: string) => page.locator(`[data-space-guests="${key}"]`);

/** Signs carol in to the organisation through Keycloak, in a browser of her own. */
async function signInAsCarol(browser: Browser, org: string): Promise<{ context: BrowserContext; page: Page }> {
  const context = await browser.newContext({ baseURL: WEB_URL, storageState: ANONYMOUS });
  const page = await context.newPage();
  await startSSO(page, org);
  await submitKeycloak(page, CAROL.username, CAROL.password);
  return { context, page };
}

/** The titles a search finds for the reader of the page, through the API the search screen asks. */
async function searchTitles(page: Page, q: string): Promise<string[]> {
  const res = await page.request.get(`/api/v1/search?q=${encodeURIComponent(q)}`);
  expect(res.status()).toBe(200);
  const { hits } = (await res.json()) as { hits: Array<{ title: Array<{ text: string }> }> };
  return hits.map((hit) => hit.title.map((segment) => segment.text).join(""));
}

// Carol is one realm user for the whole run, so this walks at one width.
test.describe("guests", { tag: ["@auth", "@desktop"] }, () => {
  test("alice lets carol into one space, carol works there and sees nothing else, and alice takes her out", async ({
    page,
    api,
    browser,
    freshOrg,
  }, testInfo) => {
    const key = uniqueKey(testInfo);
    const otherKey = `F${key.slice(1)}`;
    const word = `guestword${Date.now().toString(36)}`;
    const shared = await createSpace(api, key, "Contractor work");
    await createSpace(api, otherKey, "Inner circle");
    const brief = await createPage(api, shared.homePageId, `Brief ${word}`);
    const inner = must(await api.GET("/spaces/{spaceKey}", { params: { path: { spaceKey: otherKey } } })).space;
    await createPage(api, inner.homePageId, `Plans ${word}`);
    let carol: { context: BrowserContext; page: Page } | undefined;
    try {
      await page.goto(`/s/${key}/settings?tab=guests`);
      const card = guestsCard(page, key);
      await card.getByLabel("Email address", { exact: true }).fill(CAROL.email);
      await card.getByLabel("May", { exact: true }).selectOption("commenter");
      await card.locator('[data-action="invite-guest"]').click();
      await expect(card.getByRole("status")).toHaveText(`${CAROL.email} may sign in to this space now.`);
      await expect(card.locator(`[data-guest="${CAROL.email}"]`)).toContainText("Read and comment");
      await expectAccessible(page);

      await page.goto("/settings/sso");
      await expect(page.locator(`[data-member="${CAROL.email}"] [data-guest-space="${key}"]`)).toContainText("Contractor work");

      carol = await signInAsCarol(browser, freshOrg.slug);
      const guest = carol.page;
      await expect(guest).toHaveURL(new RegExp(`/s/${key}$`));
      await openShowing(guest, `/s/${key}`, guest.locator("main").getByRole("heading", { level: 1, name: "Contractor work", exact: true }));
      const me = (await (await guest.request.get(ME_PATH)).json()) as { organization: { role: string; guestSpace: { key: string } } };
      expect(me.organization.role).toBe("guest");
      expect(me.organization.guestSpace.key).toBe(key);
      for (const place of [guest.locator("[data-rail]"), guest.locator("[data-sidebar]")]) {
        await expect(place.getByRole("link", { name: /Spaces/ })).toHaveCount(0);
        await expect(place.getByRole("link", { name: /Search/ })).toBeVisible();
      }
      await expectAccessible(guest);

      await guest.goto(`/s/${key}/p/${brief.id}`);
      await expect(guest.locator("main").getByRole("heading", { level: 1 })).toHaveText(`Brief ${word}`);
      await guest.goto(`/s/${otherKey}`);
      await expect(guest.getByText(/not found/i).first()).toBeVisible();

      expect(await searchTitles(guest, word)).toEqual([`Brief ${word}`]);
      expect((await searchTitles(page, word)).sort()).toEqual([`Brief ${word}`, `Plans ${word}`]);
      const people = (await (await guest.request.get("/api/v1/people")).json()) as { people: Array<{ email: string }> };
      expect(people.people.map((each) => each.email)).not.toContain(BOB_EMAIL);
      expect((await guest.request.get("/api/v1/users")).status()).toBe(403);

      await page.goto(`/s/${key}/settings?tab=guests`);
      await card.locator(`[data-guest="${CAROL.email}"] [data-action="remove-guest"]`).click();
      await card.locator('[data-action="confirm-remove-guest"]').click();
      await expect(card.getByRole("status")).toContainText("was removed.");
      await expect(card.locator(`[data-guest="${CAROL.email}"]`)).toHaveCount(0);

      // Her open session stops reaching the organisation at once.
      expect((await (await guest.request.get(ME_PATH)).json()).organization ?? null).toBeNull();
    } finally {
      await carol?.context.close();
      // Deleting the space takes a guest left behind by a failure with it.
      await deleteSpace(api, key);
      await deleteSpace(api, otherKey);
    }
  });
});
