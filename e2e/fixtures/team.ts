import { randomBytes } from "node:crypto";
import { basename } from "node:path";
import { request } from "@playwright/test";
import type { Session } from "./api";
import { authTest, ME_PATH, type User } from "./auth";
import { RESETS_INSTANCE } from "./projects";
import { TEST_API_PATH, testEndpoints } from "./testapi";
import { WEB_URL } from "./stack";

/** What a person may be in the organization; a guest belongs to one space. */
export type Role = "admin" | "member" | "guest";

/** Somebody made for one spec, signed in without a provider. */
export interface Person {
  id: string;
  email: string;
  name: string;
  role: Role;
  session: Exclude<Session, string>;
}

/**
 * A spec file's own people in the one organization: alice an administrator
 * and bob a member, each named after the spec, so no other spec meets them.
 */
export interface Team {
  /** The spec's name and a random suffix, as a stub tenant or anything else of the spec's own. */
  label: string;
  /** The one organization, for what still names it: public addresses and rows written in SQL. */
  org: { id: string; slug: string };
  people: Record<User, Person>;
}

const PEOPLE_PATH = `${TEST_API_PATH}/people`;
const SUFFIX_BYTES = 4;
const ROLES: Record<User, Role> = { alice: "admin", bob: "member" };

const capitalized = (word: string) => word.charAt(0).toUpperCase() + word.slice(1);

/** Makes a person and returns them with their session; remove them with removePerson. */
export async function createPerson(label: string, name: string, role: Role, spaceKey?: string): Promise<Person> {
  const context = await testEndpoints();
  try {
    const res = await context.post(PEOPLE_PATH, { data: { label, name, role, spaceKey } });
    if (res.status() !== 201) {
      throw new Error(`POST ${PEOPLE_PATH} answered ${res.status()}: ${await res.text()}. Is the stack up with STATOR_TEST_ENDPOINTS on?`);
    }
    const person = (await res.json()) as Omit<Person, "session">;
    const session = await context.storageState();
    if (session.cookies.length === 0) throw new Error(`POST ${PEOPLE_PATH} made ${person.email} but set no session cookie.`);
    return { ...person, session };
  } finally {
    await context.dispose();
  }
}

/** Removes a person made by createPerson, and the spaces they made; one that is gone already is fine. */
export async function removePerson(id: string): Promise<void> {
  const context = await testEndpoints();
  try {
    const res = await context.delete(`${PEOPLE_PATH}/${encodeURIComponent(id)}`);
    if (res.status() !== 204 && res.status() !== 404) throw new Error(`DELETE ${PEOPLE_PATH}/${id} answered ${res.status()}: ${await res.text()}`);
  } finally {
    await context.dispose();
  }
}

async function organizationOf(person: Person): Promise<Team["org"]> {
  const context = await request.newContext({ baseURL: WEB_URL, storageState: person.session });
  try {
    const res = await context.get(ME_PATH);
    if (!res.ok()) throw new Error(`GET ${ME_PATH} as ${person.email} answered ${res.status()}: ${await res.text()}`);
    const { organization } = (await res.json()) as { organization: { id: string; slug: string } };
    return { id: organization.id, slug: organization.slug };
  } finally {
    await context.dispose();
  }
}

/** "themes.spec.ts" becomes "themes", so a leftover person names the spec that made them. */
function specOf(file: string): string {
  return basename(file).replace(/\.spec\.ts$/, "");
}

async function makeTeam(spec: string): Promise<Team> {
  const label = `${spec}-${randomBytes(SUFFIX_BYTES).toString("hex")}`;
  const users = Object.keys(ROLES) as User[];
  const made = await Promise.allSettled(users.map((user) => createPerson(`${user}-${label}`, `${capitalized(user)} ${label}`, ROLES[user])));
  const people = made.flatMap((each) => (each.status === "fulfilled" ? [each.value] : []));
  const failed = made.find((each) => each.status === "rejected");
  if (failed) {
    for (const person of people) await removePerson(person.id).catch(() => {});
    throw failed.reason;
  }
  return { label, org: await organizationOf(people[0]!), people: Object.fromEntries(users.map((user, i) => [user, people[i]!])) as Team["people"] };
}

interface TeamPool {
  /**
   * The team of one spec file in this worker, made on first use. Alone, the
   * worker is the only one running and its files never interleave, so the
   * teams of the files before go first.
   */
  forFile: (file: string, alone: boolean) => Promise<Team>;
}

async function removeTeam(pending: Promise<Team>): Promise<void> {
  const team = await pending.catch(() => undefined);
  const failures: unknown[] = [];
  for (const person of Object.values(team?.people ?? {})) await removePerson(person.id).catch((error: unknown) => failures.push(error));
  if (failures.length > 0) throw new AggregateError(failures, "Some of the specs' people could not be removed.");
}

/**
 * The test for a spec with people of its own. Its pages and API clients act as
 * the team's alice and bob, made for the spec file in this worker and removed,
 * with the spaces they made, when the worker stops. Tests of one file that land
 * in the same worker share them, one after another; other workers have their own.
 */
export const teamTest = authTest.extend<{ team: Team; makePerson: (name: string, role?: Role) => Promise<Person> }, { teamPool: TeamPool }>({
  teamPool: [
    async ({}, use) => {
      const made = new Map<string, Promise<Team>>();
      await use({
        forFile: async (file, alone) => {
          let team = made.get(file);
          if (!team) {
            if (alone) {
              for (const [other, pending] of made) {
                made.delete(other);
                await removeTeam(pending);
              }
            }
            team = makeTeam(specOf(file));
            made.set(file, team);
          }
          return team;
        },
      });
      const failures: unknown[] = [];
      for (const pending of made.values()) await removeTeam(pending).catch((error: unknown) => failures.push(error));
      if (failures.length > 0) throw new AggregateError(failures, "Some of the specs' people could not be removed.");
    },
    { scope: "worker" },
  ],
  team: async ({ teamPool }, use, testInfo) => {
    await use(await teamPool.forFile(testInfo.file, testInfo.project.metadata[RESETS_INSTANCE] === true));
  },
  makePerson: async ({ team }, use) => {
    const made: Person[] = [];
    await use(async (name, role = "member") => {
      const person = await createPerson(`${name}-${team.label}`, `${capitalized(name)} ${team.label}`, role);
      made.push(person);
      return person;
    });
    for (const person of made) await removePerson(person.id);
  },
  sessionOf: async ({ team }, use) => {
    await use((user) => team.people[user].session);
  },
});
