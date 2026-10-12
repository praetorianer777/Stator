import type { Browser } from "@playwright/test";
import type { Session } from "./api";
import { signInToOrg, USERS, type User } from "./auth";
import { TEST_API_PATH, testEndpoints } from "./testapi";

// Every spec shares the instance's one organization (fixtures/team.ts). This
// is for the one spec that needs a second, empty one: an organization's archive
// is imported only where there are no spaces yet.

/** An organisation of its own, with alice (admin) and bob (member) signed in to it. */
export interface FreshOrg {
  slug: string;
  id: string;
  sessions: Record<User, Exclude<Session, string>>;
}

const TEST_ORGS_PATH = `${TEST_API_PATH}/orgs`;

async function createOrg(label: string): Promise<{ slug: string; id: string }> {
  const context = await testEndpoints();
  try {
    const res = await context.post(TEST_ORGS_PATH, { data: { label } });
    if (res.status() !== 201) {
      throw new Error(`POST ${TEST_ORGS_PATH} answered ${res.status()}: ${await res.text()}. Is the stack up with STATOR_TEST_ENDPOINTS on?`);
    }
    return (await res.json()) as { slug: string; id: string };
  } finally {
    await context.dispose();
  }
}

async function deleteOrg(slug: string): Promise<void> {
  const context = await testEndpoints();
  try {
    const res = await context.delete(`${TEST_ORGS_PATH}/${encodeURIComponent(slug)}`);
    if (res.status() !== 204) throw new Error(`DELETE ${TEST_ORGS_PATH}/${slug} answered ${res.status()}: ${await res.text()}`);
  } finally {
    await context.dispose();
  }
}

async function makeFreshOrg(browser: Browser, label: string): Promise<FreshOrg> {
  const org = await createOrg(label);
  const users = Object.keys(USERS) as User[];
  const signedIn = await Promise.all(users.map((user) => signInToOrg(browser, user, org.slug))).catch(async (error: unknown) => {
    await deleteOrg(org.slug).catch(() => {});
    throw error;
  });
  return { ...org, sessions: Object.fromEntries(users.map((user, i) => [user, signedIn[i]!])) as FreshOrg["sessions"] };
}

/**
 * Runs fn with an organisation of its own beside the spec's, alice and bob
 * signed in to it, and removes it with all its data afterwards.
 */
export async function withAnotherOrg<T>(browser: Browser, label: string, fn: (org: FreshOrg) => Promise<T>): Promise<T> {
  const org = await makeFreshOrg(browser, label);
  try {
    return await fn(org);
  } finally {
    await deleteOrg(org.slug);
  }
}
