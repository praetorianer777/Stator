import { mkdirSync, writeFileSync } from "node:fs";
import { dirname, resolve } from "node:path";
import { fileURLToPath } from "node:url";
import { test as base, expect, request, type Browser, type Page } from "@playwright/test";
import { createStatorApi, type Session, type StatorApi } from "./api";
import { KEYCLOAK_URL, WEB_URL } from "./stack";
import { RESETS_INSTANCE } from "./projects";
import { resetInstance } from "./testapi";

// Everything about signing in lives in this file, so when the login page
// changes this is the one place to follow it.

export type User = "alice" | "bob";

/** The realm's test users, from deploy/keycloak/realm.json. */
export const USERS: Record<User, { username: string; password: string }> = {
  alice: { username: "alice", password: "alice password" },
  bob: { username: "bob", password: "bob password" },
};

/** The owner the server bootstraps, the one account that signs in with a password. */
export const BOOTSTRAP_OWNER = { email: "admin@stator.test", password: "stator admin password" };

/** What the login page offers, and what answers once someone is signed in. */
export const LOGIN_PATH = "/login";
export const ME_PATH = "/api/v1/auth/me";
const SSO_START = '[data-action="sso"]';
const PASSWORD_SUBMIT = '[data-action="password-sign-in"]';

// A click that starts a navigation waits for it within the action timeout.
// The api's sign-in hops commit several times and redeem the code with the
// provider, which on a busy machine outlasted that; so these clicks return at
// once, and the next step waits for the page within the navigation timeout.
const LEAVES_THE_PAGE = { noWaitAfter: true } as const;

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
 * Starts SSO from the login page, staying on it when a redirect brought the browser
 * there so its `next` is kept; the caller waits for the page the provider leads to.
 */
export async function startSSO(page: Page): Promise<void> {
  if (!page.url().startsWith(`${WEB_URL}${LOGIN_PATH}`)) await page.goto(LOGIN_PATH);
  await page.locator(SSO_START).click(LEAVES_THE_PAGE);
}

/** Fills Keycloak's own form, then waits until the browser is back in Stator. */
export async function submitKeycloak(page: Page, username: string, password: string): Promise<void> {
  await page.waitForURL(`${KEYCLOAK_URL}/**`);
  await page.locator("#username").fill(username);
  await page.locator("#password").fill(password);
  await page.locator("#kc-login").click(LEAVES_THE_PAGE);
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
export async function signInToOrg(browser: Browser, user: User, _org: string): Promise<Exclude<Session, string>> {
  const context = await browser.newContext({ baseURL: WEB_URL, storageState: stateFile(user) });
  try {
    const page = await context.newPage();
    await page.goto("/api/v1/auth/oidc/start");
    if (page.url().startsWith(KEYCLOAK_URL)) await submitKeycloak(page, USERS[user].username, USERS[user].password);
    await expect
      .poll(async () => {
        const me = await page.request.get(ME_PATH);
        return me.ok() ? Boolean(((await me.json()) as { organization?: object | null }).organization) : me.status();
      })
      .toBe(true);
    return await context.storageState();
  } finally {
    await context.close();
  }
}

/** Somebody besides alice and bob, such as a person a test made for itself. */
export interface SignedIn {
  session: Session;
}

function sessionFor(sessionOf: (user: User) => Session, who: User | SignedIn): Session {
  return typeof who === "string" ? sessionOf(who) : who.session;
}

interface SessionFixtures {
  /** Where each user's session comes from: the setup's, or the spec's own people's. */
  sessionOf: (user: User) => Session;
  /** A page of its own, signed in as the user, beside the default page as alice. */
  pageAs: (who: User | SignedIn) => Promise<Page>;
  /** The API as alice. */
  api: StatorApi;
  /** The API as the user. */
  apiAs: (who: User | SignedIn) => Promise<StatorApi>;
  /** Puts the organization's settings back when a file of the instance projects starts. */
  instanceSettings: undefined;
}

// A worker is a process of its own, so this is the file it last reset for.
// The repeat is part of it: --repeat-each runs a file's repeats back to back.
let resetFor: string | undefined;

/**
 * The test for the shell: alice's page once the stack can sign in, an anonymous
 * one before, so the same specs hold on both sides of the login wall.
 */
export const test = base.extend<SessionFixtures>({
  instanceSettings: [
    async ({}, use, testInfo) => {
      const file = `${testInfo.file}#${testInfo.repeatEachIndex}`;
      if (testInfo.project.metadata[RESETS_INSTANCE] === true && resetFor !== file) {
        await resetInstance();
        resetFor = file;
      }
      await use(undefined);
    },
    { auto: true },
  ],
  sessionOf: async ({}, use) => {
    await use(stateFile);
  },
  storageState: async ({ sessionOf }, use) => {
    await use(sessionOf("alice"));
  },
  pageAs: async ({ browser, viewport, hasTouch, isMobile, sessionOf }, use) => {
    const opened: Array<{ close: () => Promise<void> }> = [];
    await use(async (user) => {
      const context = await browser.newContext({ baseURL: WEB_URL, storageState: sessionFor(sessionOf, user), viewport, hasTouch, isMobile });
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
      const api = await createStatorApi(sessionFor(sessionOf, user));
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
