import { mkdirSync, writeFileSync } from "node:fs";
import { dirname, resolve } from "node:path";
import { fileURLToPath } from "node:url";
import { test as base, expect, request, type Browser, type Page } from "@playwright/test";
import { createStatorApi, type Session, type StatorApi } from "./api";
import { KEYCLOAK_URL, WEB_URL } from "./stack";

// Everything about signing in lives in this file, so when the login page
// changes this is the one place to follow it.

export type User = "alice" | "bob";

/** The realm's test users, from deploy/keycloak/realm.json. */
export const USERS: Record<User, { username: string; password: string }> = {
  alice: { username: "alice", password: "alice password" },
  bob: { username: "bob", password: "bob password" },
};

/** The organisation the seed makes. */
export const ORG_SLUG = "demo";

/** The owner the server bootstraps, the one account that signs in with a password. */
export const BOOTSTRAP_OWNER = { email: "admin@stator.test", password: "stator admin password" };

/** What the login page offers, and what answers once someone is signed in. */
export const LOGIN_PATH = "/login";
export const ME_PATH = "/api/v1/auth/me";
const ORG_FIELD = "Organization";
const SSO_START = '[data-action="sso"]';
const PASSWORD_SUBMIT = '[data-action="password-sign-in"]';

export const AUTH_MISSING = `Sign-in is not built yet: GET ${ME_PATH} answers 404 (issue #5).`;

const AUTH_DIR = resolve(dirname(fileURLToPath(import.meta.url)), "../.auth");

/** Where a user's signed-in browser state is kept between the setup and the specs. */
export function stateFile(user: User): string {
  return resolve(AUTH_DIR, `${user}.json`);
}

let available: Promise<boolean> | undefined;

/** Whether the stack can sign anyone in: the session endpoint exists once #5 lands. */
export function authAvailable(): Promise<boolean> {
  available ??= (async () => {
    const context = await request.newContext({ baseURL: WEB_URL });
    try {
      return (await context.get(ME_PATH)).status() !== 404;
    } finally {
      await context.dispose();
    }
  })();
  return available;
}

/**
 * Starts SSO for an organisation from the login page, staying on it when a redirect
 * already brought the browser there, so its `next` is kept.
 */
export async function startSSO(page: Page, org: string = ORG_SLUG): Promise<void> {
  if (!page.url().startsWith(`${WEB_URL}${LOGIN_PATH}`)) await page.goto(LOGIN_PATH);
  await page.getByLabel(ORG_FIELD, { exact: true }).fill(org);
  await page.locator(SSO_START).click();
}

/** Fills Keycloak's own form, then waits until the browser is back in Stator. */
export async function submitKeycloak(page: Page, username: string, password: string): Promise<void> {
  await page.waitForURL(`${KEYCLOAK_URL}/**`);
  await page.locator("#username").fill(username);
  await page.locator("#password").fill(password);
  await page.locator("#kc-login").click();
  await page.waitForURL((url) => url.origin === new URL(WEB_URL).origin && !url.pathname.startsWith("/api/"));
}

/** Signs in through the login page and Keycloak's form, and waits for the session. */
export async function signIn(page: Page, user: User): Promise<void> {
  await startSSO(page);
  await submitKeycloak(page, USERS[user].username, USERS[user].password);
  await expect.poll(async () => (await page.request.get(ME_PATH)).status()).toBe(200);
}

/** Signs the bootstrap owner in with the password form on the login page. */
export async function signInWithPassword(page: Page, email = BOOTSTRAP_OWNER.email, password = BOOTSTRAP_OWNER.password): Promise<void> {
  await page.goto(LOGIN_PATH);
  await page.getByLabel("Email", { exact: true }).fill(email);
  await page.getByLabel("Password", { exact: true }).fill(password);
  await page.locator(PASSWORD_SUBMIT).click();
  await expect.poll(async () => (await page.request.get(ME_PATH)).status()).toBe(200);
}

/**
 * Signs the user in and stores the session for the specs. Without sign-in the
 * file holds an empty state, so a spec that loads it still starts.
 */
export async function saveSession(browser: Browser, user: User): Promise<void> {
  mkdirSync(AUTH_DIR, { recursive: true });
  if (!(await authAvailable())) {
    writeFileSync(stateFile(user), JSON.stringify({ cookies: [], origins: [] }));
    return;
  }
  const context = await browser.newContext({ baseURL: WEB_URL, storageState: { cookies: [], origins: [] } });
  try {
    await signIn(await context.newPage(), user);
    await context.storageState({ path: stateFile(user) });
  } finally {
    await context.close();
  }
}

/**
 * Signs the user in to another organization through its provider and returns the
 * session, in memory. Keycloak still knows them from the setup's sign-in, so it
 * usually sends the browser straight back; its form is filled when it asks.
 */
export async function signInToOrg(browser: Browser, user: User, org: string): Promise<Exclude<Session, string>> {
  const context = await browser.newContext({ baseURL: WEB_URL, storageState: stateFile(user) });
  try {
    const page = await context.newPage();
    await page.goto(`/api/v1/auth/oidc/${encodeURIComponent(org)}/start`);
    if (page.url().startsWith(KEYCLOAK_URL)) await submitKeycloak(page, USERS[user].username, USERS[user].password);
    await expect
      .poll(async () => {
        const me = await page.request.get(ME_PATH);
        return me.ok() ? ((await me.json()) as { organization?: { slug?: string } }).organization?.slug : me.status();
      })
      .toBe(org);
    return await context.storageState();
  } finally {
    await context.close();
  }
}

interface SessionFixtures {
  /** Where each user's session comes from: the setup's, or a fresh organization's. */
  sessionOf: (user: User) => Session;
  /** A page of its own, signed in as the user, beside the default page as alice. */
  pageAs: (user: User) => Promise<Page>;
  /** The API as alice. */
  api: StatorApi;
  /** The API as the user. */
  apiAs: (user: User) => Promise<StatorApi>;
}

/**
 * The test for the shell: alice's page once the stack can sign in, an anonymous
 * one before, so the same specs hold on both sides of the login wall.
 */
export const test = base.extend<SessionFixtures>({
  sessionOf: async ({}, use) => {
    await use(stateFile);
  },
  storageState: async ({ sessionOf }, use) => {
    await use(sessionOf("alice"));
  },
  pageAs: async ({ browser, viewport, hasTouch, isMobile, sessionOf }, use) => {
    const opened: Array<{ close: () => Promise<void> }> = [];
    await use(async (user) => {
      const context = await browser.newContext({ baseURL: WEB_URL, storageState: sessionOf(user), viewport, hasTouch, isMobile });
      opened.push(context);
      return context.newPage();
    });
    for (const context of opened) await context.close();
  },
  api: async ({ sessionOf }, use) => {
    const api = await createStatorApi(sessionOf("alice"));
    await use(api);
    await api.dispose();
  },
  apiAs: async ({ sessionOf }, use) => {
    const opened: StatorApi[] = [];
    await use(async (user) => {
      const api = await createStatorApi(sessionOf(user));
      opened.push(api);
      return api;
    });
    for (const api of opened) await api.dispose();
  },
});

/** The test for specs tagged @auth: skipped, with the reason, until the stack can sign anyone in. */
export const authTest = test.extend<{ requireAuth: undefined }>({
  requireAuth: [
    async ({}, use, testInfo) => {
      testInfo.skip(!(await authAvailable()), AUTH_MISSING);
      await use(undefined);
    },
    { auto: true },
  ],
});

export { expect };
