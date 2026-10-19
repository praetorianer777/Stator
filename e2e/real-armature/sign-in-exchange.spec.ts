import { request, type APIRequestContext } from "@playwright/test";
import { must } from "../fixtures/api";
import { ME_PATH, expect, startSSO, submitKeycloak } from "../fixtures/auth";
import { KEYCLOAK_URL, WEB_URL } from "../fixtures/stack";
import { teamTest as test } from "../fixtures/team";
import { answered, armatureSignedIn, connectSite, passwordOf, readSeed, realArmatureURL } from "./armature";

// Connected at sign-in (#430): Stator and the real Armature share the realm
// dynamo, so alice signing in to Stator through Keycloak is connected to
// Armature at once, and reads an Armature issue without clicking Connect or
// pasting a token. Stator exchanges her access token at Armature's token
// endpoint as its connected app, whose Keycloak client an Armature
// administrator named.

const EXCHANGE = "oauth.token-exchange@1";
const ANONYMOUS = { cookies: [], origins: [] };
const REALM = "dynamo";
// Development-only, from deploy/keycloak/realm.json.
const ARMATURE_CLIENT = { clientId: "armature", clientSecret: "armature-dev-secret" };
const STATOR_CLIENT = "stator";
const ALICE = { username: "alice", password: "alice password", email: "alice@suite.test", name: "Alice Admin" };
const KEYCLOAK_ADMIN_PASSWORD = process.env.STATOR_TEST_KEYCLOAK_ADMIN_PASSWORD ?? "admin";

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

/** The realm's admin API, as Keycloak's administrator. */
async function keycloakAdmin(): Promise<APIRequestContext> {
  const login = await request.newContext({ baseURL: KEYCLOAK_URL });
  const token = await answered<{ access_token: string }>(
    login.post("realms/master/protocol/openid-connect/token", {
      form: { grant_type: "password", client_id: "admin-cli", username: "admin", password: KEYCLOAK_ADMIN_PASSWORD },
    }),
    200,
    "Keycloak's administrator signs in",
  );
  await login.dispose();
  return request.newContext({
    baseURL: `${KEYCLOAK_URL}/admin/realms/${REALM}/`,
    extraHTTPHeaders: { Authorization: `Bearer ${token.access_token}` },
  });
}

test.describe("connected to the real Armature at sign-in", { tag: ["@auth"] }, () => {
  test("alice signs in to Stator through the realm and reads an Armature issue without connecting", async ({ api, browser }) => {
    const seed = readSeed();
    const anybody = await request.newContext({ baseURL: `${realArmatureURL()}/api/v1/` });
    const meta = await answered<{ capabilities: string[] }>(anybody.get("meta"), 200, "what Armature offers");
    await anybody.dispose();
    test.skip(
      !meta.capabilities.includes(EXCHANGE),
      `The pinned Armature does not report ${EXCHANGE} yet (Cloudster1/Armature#302); move api/armature/SOURCE past it with make armature-openapi.`,
    );

    const admin = await armatureSignedIn(seed.people.admin.email);
    const keycloak = await keycloakAdmin();
    const cleanups: Array<() => Promise<unknown>> = [];
    try {
      await answered(admin.put("oidc-provider", { data: provider(true) }), 200, "point Armature at the realm");
      cleanups.push(() => admin.put("oidc-provider", { data: provider(false) }));
      // Armature lets nobody in by the provider's word alone; an account under
      // her address is what makes alice somebody it connects.
      const alice = await request.newContext({ baseURL: `${realArmatureURL()}/api/v1/` });
      const password = passwordOf(ALICE.email);
      let res = await alice.post("auth/signup", { data: { email: ALICE.email, password, name: ALICE.name } });
      if (!res.ok()) res = await alice.post("auth/login", { data: { email: ALICE.email, password } });
      expect(res.ok(), `alice's account in Armature: ${res.status()} ${await res.text()}`).toBe(true);
      await alice.dispose();

      // An Armature administrator adds Stator under Settings, Connected apps,
      // naming its Keycloak client, which consents for everybody.
      const { client } = await answered<{ client: { id: string; secret: string } }>(
        admin.post("oauth/clients", {
          data: {
            name: "Stator",
            redirectUris: [`${WEB_URL}/api/v1/armature/oauth/callback`],
            scopes: ["read", "issue.write"],
            confidential: true,
            keycloakClientId: STATOR_CLIENT,
          },
        }),
        201,
        "add Stator as a connected app",
      );
      cleanups.push(() => admin.delete(`oauth/clients/${client.id}`));
      await connectSite(api, seed);
      cleanups.push(() => api.DELETE("/armature/connection"));
      must(await api.PUT("/armature/connection/app", { body: { clientId: client.id, clientSecret: client.secret } }));

      // Armature takes a token whose audience holds its own client.
      const clients = await answered<Array<{ id: string }>>(keycloak.get("clients", { params: { clientId: STATOR_CLIENT } }), 200, "Stator's client");
      const statorClient = clients[0]?.id;
      if (!statorClient) throw new Error(`The realm ${REALM} has no client ${STATOR_CLIENT}.`);
      const mapper = `armature-audience-${Date.now()}`;
      await answered(
        keycloak.post(`clients/${statorClient}/protocol-mappers/models`, {
          data: {
            name: mapper,
            protocol: "openid-connect",
            protocolMapper: "oidc-audience-mapper",
            config: { "included.client.audience": ARMATURE_CLIENT.clientId, "access.token.claim": "true", "id.token.claim": "false" },
          },
        }),
        201,
        "add Armature to the audience of Stator's tokens",
      );
      cleanups.push(async () => {
        const mappers = await answered<Array<{ id: string; name: string }>>(
          keycloak.get(`clients/${statorClient}/protocol-mappers/models`),
          200,
          "the mappers",
        );
        for (const each of mappers.filter((one) => one.name === mapper)) await keycloak.delete(`clients/${statorClient}/protocol-mappers/models/${each.id}`);
      });

      const context = await browser.newContext({ baseURL: WEB_URL, storageState: ANONYMOUS });
      cleanups.push(() => context.close());
      const page = await context.newPage();
      await startSSO(page);
      await submitKeycloak(page, ALICE.username, ALICE.password);
      expect((await (await page.request.get(ME_PATH)).json()).user.email).toBe(ALICE.email);

      const { account } = await (await page.request.get("/api/v1/armature/account")).json();
      expect(account, "alice's link to Armature after signing in").toMatchObject({ connected: true, via: "app", status: "ok", app: { signInRefusal: null } });
      const issue = await (await page.request.get(`/api/v1/armature/issues/${seed.issues.guide.key}`)).json();
      expect(issue).toMatchObject({ status: "ok", issue: { key: seed.issues.guide.key, summary: seed.issues.guide.summary } });

      await page.goto("/settings/profile");
      await expect(page.locator("[data-armature-account] [data-armature-user]")).toHaveText(`Connected as ${ALICE.name} (${ALICE.email}).`);
    } finally {
      for (const cleanup of cleanups.reverse()) await cleanup().catch(() => undefined);
      await keycloak.dispose();
      await admin.dispose();
    }
  });
});
