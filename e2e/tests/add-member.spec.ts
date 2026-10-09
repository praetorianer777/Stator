import { must } from "../fixtures/api";
import { ME_PATH, authTest as test, expect, signInWithPassword } from "../fixtures/auth";
import { expectAccessible } from "../fixtures/shell";
import { WEB_URL } from "../fixtures/stack";

const ANONYMOUS = { cookies: [], origins: [] };
const SSO_PATH = "/settings/sso";

test.describe("adding people with a password", { tag: ["@auth", "@desktop"] }, () => {
  const added: string[] = [];

  test.afterEach(async ({ api }) => {
    const members = must(await api.GET("/users")).members;
    for (const email of added.splice(0)) {
      const made = members.find((each) => each.email === email);
      if (made) await api.DELETE("/users/{userID}", { params: { path: { userID: made.userId } } });
    }
  });

  test("an administrator adds somebody, who signs in with the password shown once", async ({ browser, page }, testInfo) => {
    const email = `added-${testInfo.workerIndex}-${Date.now().toString(36)}@stator.test`;
    added.push(email);

    await page.goto(SSO_PATH);
    const card = page.locator("[data-members]");
    await card.getByRole("button", { name: "Add person" }).click();
    await card.getByLabel("Email address").fill(email);
    await expectAccessible(page);
    await card.getByRole("button", { name: "Add", exact: true }).click();

    const shown = card.locator("[data-new-password]");
    await expect(shown).toBeVisible();
    const password = (await shown.textContent())!.trim();
    expect(password.length).toBeGreaterThanOrEqual(12);
    await expect(card.locator(`[data-member="${email}"]`)).toBeVisible();

    const context = await browser.newContext({ baseURL: WEB_URL, storageState: ANONYMOUS });
    const fresh = await context.newPage();
    await signInWithPassword(fresh, email, password);
    expect((await fresh.request.get(ME_PATH)).status()).toBe(200);
    await context.close();

    // Shown once: a reload has nothing to show.
    await page.reload();
    await expect(page.locator("[data-members]").locator("[data-new-password]")).toHaveCount(0);
  });

  test("a password that is too short is refused beside its field and nobody is added", async ({ page, api }, testInfo) => {
    const email = `short-${testInfo.workerIndex}-${Date.now().toString(36)}@stator.test`;
    added.push(email);

    await page.goto(SSO_PATH);
    const card = page.locator("[data-members]");
    await card.getByRole("button", { name: "Add person" }).click();
    await card.getByLabel("Email address").fill(email);
    await card.getByLabel("Password").fill("short");
    await card.getByRole("button", { name: "Add", exact: true }).click();
    await expect(card.getByText(/at least 12 characters/)).toBeVisible();
    expect(must(await api.GET("/users")).members.some((each) => each.email === email)).toBe(false);
  });
});
