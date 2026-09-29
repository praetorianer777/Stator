import { test as setup } from "@playwright/test";
import { saveSession } from "../fixtures/auth";

// Signs everybody in once, before any spec, so each @auth spec starts from a
// stored session instead of walking through Keycloak itself.
setup("sign in alice and bob", async ({ browser }) => {
  await saveSession(browser, "alice");
  await saveSession(browser, "bob");
});
