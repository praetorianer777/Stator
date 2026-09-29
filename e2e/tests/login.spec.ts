import { request, type Page } from "@playwright/test";
import { LOGIN_PATH, ME_PATH, authTest as test, expect, signIn, signInWithPassword, startSSO, stateFile, submitKeycloak } from "../fixtures/auth";
import { orgWithDeadProvider } from "../fixtures/db";
import { expectAccessible, startInScheme, type ColourScheme } from "../fixtures/shell";
import { WEB_URL } from "../fixtures/stack";

const ANONYMOUS = { cookies: [], origins: [] };
const SCHEMES: ColourScheme[] = ["light", "dark"];

const accountItem = (page: Page, action: string) => page.locator(`[role="menu"] [data-action="${action}"]`);

// The harness proving itself, and the sign-in flows a browser alone can walk.
test.describe("sign-in", { tag: "@auth" }, () => {
  test("the stored session is alice's, and the shell names her with her initials", async ({ page, api }) => {
    expect((await page.request.get(ME_PATH)).status()).toBe(200);
    await page.goto("/");
    const account = page.locator('[data-action="account"]');
    await expect(account).toContainText("AA");
    await expect(account).not.toContainText("Guest");
    const { response } = await api.GET("/themes");
    expect(response.status).toBe(200);
  });

  test("an admin finds single sign-on in the account menu, and a member does not", async ({ page, pageAs }) => {
    await page.goto("/");
    await page.locator('[data-action="account"]').click();
    // Contains, not equals: a count of people waiting may follow the words.
    await expect(accountItem(page, "sso-settings")).toContainText("Single sign-on");

    const bob = await pageAs("bob");
    await bob.goto("/");
    await bob.locator('[data-action="account"]').click();
    await expect(bob.getByRole("menu")).toBeVisible();
    await expect(accountItem(bob, "sso-settings")).toHaveCount(0);
  });

  test("the SSO settings show the callback address, and a save with a blank secret keeps the stored one", async ({ page, browser }) => {
    await page.goto("/settings/sso");
    await expect(page.getByText("Register this redirect address with the provider:")).toBeVisible();
    await expect(page.locator("form code").first()).toContainText("/api/v1/auth/oidc/");
    const secret = page.getByLabel("Client secret", { exact: true });
    await expect(secret).toHaveValue("");
    await expect(secret).toHaveAttribute("placeholder", "Stored. Leave blank to keep it.");

    const saved = page.waitForResponse((res) => res.url().endsWith("/api/v1/oidc-provider") && res.request().method() === "PUT");
    await page.locator('[data-action="save-sso"]').click();
    expect((await saved).status()).toBe(200);
    await expect(page.getByLabel("Client secret", { exact: true })).toHaveAttribute("placeholder", "Stored. Leave blank to keep it.");

    // The proof the secret survived: Keycloak still accepts the client.
    const fresh = await browser.newContext({ baseURL: WEB_URL, storageState: ANONYMOUS });
    await signIn(await fresh.newPage(), "bob");
    await fresh.close();
  });

  for (const scheme of SCHEMES) {
    test(`/settings/sso passes axe in ${scheme}`, async ({ page }) => {
      await startInScheme(page, scheme);
      await page.goto("/settings/sso");
      await expect(page.locator('[data-action="save-sso"]')).toBeVisible();
      await expectAccessible(page);
    });
  }

  test("a session that ends on the server sends the next request to the login page and back", async ({ browser }) => {
    const context = await browser.newContext({ baseURL: WEB_URL, storageState: ANONYMOUS });
    const page = await context.newPage();
    await signIn(page, "bob");
    await page.goto("/spaces");
    await expect(page.getByRole("heading", { level: 1, name: "Spaces" })).toBeVisible();

    // Signed out behind the browser's back: its cookie now names a session the api has dropped.
    const elsewhere = await request.newContext({ baseURL: WEB_URL, storageState: await context.storageState(), extraHTTPHeaders: { Origin: WEB_URL } });
    expect((await elsewhere.post("/api/v1/auth/logout")).ok()).toBe(true);
    await elsewhere.dispose();

    await page.locator('[data-action="account"]').click();
    await accountItem(page, "themes").click();
    await expect(page).toHaveURL(/\/login\?next=%2Fsettings%2Fthemes/);
    await context.close();
  });

  test.describe("from a fresh browser", () => {
    test.use({ storageState: ANONYMOUS });

    test("the login page signs bob in through Keycloak", async ({ page }) => {
      expect((await page.request.get(ME_PATH)).status()).toBe(401);
      await signIn(page, "bob");
      await page.goto("/");
      await expect(page.locator("main#main")).toBeVisible();
    });

    test("the bootstrap owner signs in with a password", async ({ page }) => {
      await signInWithPassword(page);
      await page.goto("/");
      await expect(page.locator("main#main")).toBeVisible();
    });

    test("a visitor opening /spaces signs in and comes back to /spaces", async ({ page }) => {
      await page.goto("/spaces");
      await expect(page).toHaveURL(/\/login\?next=%2Fspaces$/);
      await startSSO(page);
      await submitKeycloak(page, "alice", "alice password");
      await expect(page).toHaveURL(/\/spaces$/);
      await expect(page.getByRole("heading", { level: 1, name: "Spaces" })).toBeVisible();
    });

    test("the account menu shows Alice Admin, and signing out closes the way back in", async ({ page }) => {
      await signIn(page, "alice");
      await page.goto("/");
      await expect(page.locator("[data-account-name]")).toHaveText("Alice Admin");
      await page.locator('[data-action="account"]').click();
      await accountItem(page, "sign-out").click();
      await expect(page).toHaveURL(new RegExp(`${LOGIN_PATH}(\\?|$)`));
      await page.goto("/");
      await expect(page).toHaveURL(new RegExp(`${LOGIN_PATH}\\?next=`));
    });

    test("an organisation without SSO sends the visitor back with a message", async ({ page }) => {
      await startSSO(page, "no-such-org");
      await expect(page).toHaveURL(/\/login\?sso=not_configured/);
      await expect(page.getByRole("alert")).toHaveText("That organization does not sign in with SSO. Check its name, or ask one of its administrators.");
    });

    test("an organisation whose provider cannot be reached says so", async ({ page }, testInfo) => {
      const org = await orgWithDeadProvider(testInfo);
      try {
        await startSSO(page, org.slug);
        await expect(page).toHaveURL(/\/login\?sso=unreachable/);
        await expect(page.getByRole("alert")).toHaveText(
          "Stator could not reach your organization's identity provider. Try again in a moment, and tell an administrator if it keeps failing.",
        );
        expect((await page.request.get(ME_PATH)).status()).toBe(401);
      } finally {
        await org.remove();
      }
    });

    for (const scheme of SCHEMES) {
      test(`/login passes axe in ${scheme}`, async ({ page }) => {
        await startInScheme(page, scheme);
        await page.goto(LOGIN_PATH);
        await expect(page.locator("[data-login]")).toBeVisible();
        await expectAccessible(page);
      });
    }
  });
});

// Kept beside the flows so a change of where sessions are stored shows up here first.
test("the setup stored a session for every user", { tag: "@auth" }, async () => {
  for (const user of ["alice", "bob"] as const) {
    const context = await request.newContext({ baseURL: WEB_URL, storageState: stateFile(user) });
    expect((await context.get(ME_PATH)).status(), user).toBe(200);
    await context.dispose();
  }
});
