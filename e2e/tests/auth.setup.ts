import { test as setup } from "@playwright/test";
import { saveSession } from "../fixtures/auth";
import { clearDemoThemes } from "../fixtures/seed";
import { resetInstance } from "../fixtures/testapi";

// Signs everybody in once, before any spec, so each @auth spec starts from a
// stored session instead of walking through Keycloak itself. The organization
// then has the bootstrap's settings and shows the built-in theme, whatever an
// earlier run that stopped halfway left there.
setup("sign in alice and bob", async ({ browser }) => {
  await saveSession(browser, "alice");
  await saveSession(browser, "bob");
  await resetInstance();
  await clearDemoThemes();
});
