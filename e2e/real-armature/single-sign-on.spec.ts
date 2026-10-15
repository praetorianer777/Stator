import { request, type APIRequestContext } from "@playwright/test";
import { ME_PATH, authTest as test, expect, startSSO, submitKeycloak } from "../fixtures/auth";
import { KEYCLOAK_URL, WEB_URL } from "../fixtures/stack";
import { answered, armatureSignedIn, passwordOf, readSeed, realArmatureURL } from "./armature";

// The stack's Keycloak serves the suite's realm dynamo, with a client for
// Armature beside Stator's (#411). Pointed at it, the real Armature lets alice
// in on the strength of the sign-in she just made in Stator: one sign-in, two
// tools, and no second password form.

const ANONYMOUS = { cookies: [], origins: [] };
const REALM = "dynamo";
// Development-only, from deploy/keycloak/realm.json.
const ARMATURE_CLIENT = { clientId: "armature", clientSecret: "armature-dev-secret" };
const ALICE = { username: "alice", password: "alice password", email: "alice@suite.test", name: "Alice Admin" };

function provider(enabled: boolean) {
  return {
    issuer: `${KEYCLOAK_URL}/realms/${REALM}`,
    ...ARMATURE_CLIENT,
    groupsClaim: "groups",
    scopes: "openid profile email",
    createGroups: false,
    enabled,
  };
}

test.describe("one Keycloak for Stator and Armature", { tag: ["@auth"] }, () => {
  let admin: APIRequestContext;

  test.beforeAll(async () => {
    admin = await armatureSignedIn(readSeed().people.admin.email);
    await answered(admin.put("oidc-provider", { data: provider(true) }), 200, "point Armature at the realm");
    // Armature lets nobody in by the provider's word alone; an account under
    // her address is what makes alice a member there.
    // Signs her up, or in when a kept Armature (KEEP_ARMATURE=1) has her already.
    const alice = await request.newContext({ baseURL: `${realArmatureURL()}/api/v1/` });
    const password = passwordOf(ALICE.email);
    let res = await alice.post("auth/signup", { data: { email: ALICE.email, password, name: ALICE.name } });
    if (!res.ok()) res = await alice.post("auth/login", { data: { email: ALICE.email, password } });
    expect(res.ok(), `alice's account in Armature: ${res.status()} ${await res.text()}`).toBe(true);
    await alice.dispose();
  });

  // The other specs sign in to Armature with passwords; leave it as they expect.
  test.afterAll(async () => {
    await answered(admin.put("oidc-provider", { data: provider(false) }), 200, "switch Armature's single sign-on off again");
    await admin.dispose();
  });

  test("alice signs in to Stator through the realm, and Armature takes her in without asking again", async ({ browser }) => {
    const context = await browser.newContext({ baseURL: WEB_URL, storageState: ANONYMOUS });
    try {
      const page = await context.newPage();
      await startSSO(page);
      await submitKeycloak(page, ALICE.username, ALICE.password);
      const stator = await page.request.get(ME_PATH);
      expect(stator.status()).toBe(200);
      expect((await stator.json()).user.email).toBe(ALICE.email);

      // Keycloak's session from the sign-in above answers with redirects alone;
      // a page of Keycloak's that renders is its form asking again.
      const armature = realArmatureURL();
      const keycloakPages: string[] = [];
      page.on("framenavigated", (frame) => {
        if (frame === page.mainFrame() && frame.url().startsWith(`${KEYCLOAK_URL}/`)) keycloakPages.push(frame.url());
      });
      await page.goto(`${armature}/api/v1/auth/oidc/start`);
      expect(keycloakPages, "Keycloak showed a page of its own instead of recognising alice").toEqual([]);
      expect(new URL(page.url()).origin, `landed on ${page.url()}`).toBe(new URL(armature).origin);
      expect(page.url()).not.toContain("sso=");

      const me = await page.request.get(`${armature}/api/v1/auth/me`);
      expect(me.status(), await me.text()).toBe(200);
      expect((await me.json()).principal.user.email).toBe(ALICE.email);
    } finally {
      await context.close();
    }
  });
});
