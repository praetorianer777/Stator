import { existsSync, readFileSync } from "node:fs";
import { dirname, resolve } from "node:path";
import { fileURLToPath } from "node:url";
import { expect, request, type APIRequestContext } from "@playwright/test";
import { must, type StatorApi } from "../fixtures/api";
import { STACK_ENV_FILE } from "../fixtures/stack";

// The real Armature make test-armature starts beside the stack, and what the
// seed made in it. Nothing here stands in for Armature: every call below is
// one Armature's own API answers.

/** Where seed.setup.ts leaves what it made, for the specs of the same run. */
export const SEED_FILE = resolve(dirname(fileURLToPath(import.meta.url)), "../../.cache/armature-seed.json");

/** The api as Armature's worker reaches it on the stack's network, where its webhooks go. */
export const WEBHOOK_URL = "http://api:8080/api/v1/armature/webhook";

/** The topics Stator acts on, which the connection screen asks an administrator to subscribe to. */
export const WEBHOOK_TOPICS = ["issue.created", "issue.updated", "issue.transitioned", "comment.added"];

/** The real Armature as a browser opens it, which is the address an administrator connects. */
export function realArmatureURL(): string {
  const fromEnv = process.env.STATOR_REAL_ARMATURE_URL;
  if (fromEnv) return fromEnv;
  if (existsSync(STACK_ENV_FILE)) {
    const line = readFileSync(STACK_ENV_FILE, "utf8")
      .split("\n")
      .find((each) => each.startsWith("STATOR_REAL_ARMATURE_URL="));
    if (line) return line.slice(line.indexOf("=") + 1).trim();
  }
  throw new Error("STATOR_REAL_ARMATURE_URL is not set. Run these specs with make test-armature, which starts Armature and names it.");
}

/** Somebody in Armature, with the personal access token the seed made for them. */
export interface ArmaturePerson {
  id: string;
  name: string;
  email: string;
  token: string;
}

/** An issue the seed filed, by its key and summary. */
export interface SeededIssue {
  key: string;
  summary: string;
}

/** What the seed made in Armature. */
export interface Seed {
  url: string;
  /** The installation /auth/me names, which Stator's connection learns and checks every token against. */
  installationId: string;
  project: { key: string; name: string };
  team: { id: string; name: string };
  /** The administrator who arranges things in Armature, Mia who sees the project, Nils who does not. */
  people: { admin: ArmaturePerson; member: ArmaturePerson; outsider: ArmaturePerson };
  issues: { epic: SeededIssue; guide: SeededIssue; runbooks: SeededIssue; signIn: SeededIssue };
  /** Days the dated issues start and end on, as YYYY-MM-DD. */
  dates: { guideStart: string; guideDue: string; runbooksStart: string; runbooksDue: string; signInDue: string };
  /** The secret Armature showed for the webhook to Stator. */
  webhookSecret: string;
  /** The theme Mia uses in Armature. */
  theme: { id: string; name: string };
}

export function readSeed(): Seed {
  if (!existsSync(SEED_FILE)) throw new Error(`${SEED_FILE} is missing; the armature-seed project writes it before the specs run.`);
  return JSON.parse(readFileSync(SEED_FILE, "utf8")) as Seed;
}

/** Armature's API as one person, by their token; dispose it when done. */
export async function armatureAs(token: string): Promise<APIRequestContext> {
  return request.newContext({ baseURL: `${realArmatureURL()}/api/v1/`, extraHTTPHeaders: { Authorization: `Bearer ${token}` } });
}

/** The password the seed signs each of its people up with. */
export const passwordOf = (email: string) => `${email} armature password`;

/**
 * Armature's API as somebody signed in with their password. A personal access
 * token holds less than its owner: an administrator's cannot read the webhook log.
 */
export async function armatureSignedIn(email: string): Promise<APIRequestContext> {
  const context = await request.newContext({ baseURL: `${realArmatureURL()}/api/v1/` });
  await answered(context.post("auth/login", { data: { email, password: passwordOf(email) } }), 200, `${email} signs in`);
  return context;
}

/** The JSON of an answer, failing with Armature's own words when the status is not the one wanted. */
export async function answered<T = Record<string, unknown>>(pending: ReturnType<APIRequestContext["fetch"]>, status: number, what: string): Promise<T> {
  const res = await pending;
  const text = await res.text();
  expect(res.status(), `${what}: Armature answered ${res.status()} ${text}`).toBe(status);
  return (text ? JSON.parse(text) : null) as T;
}

/** Connects the organization to the real Armature, with the webhook secret the seed was shown. */
export async function connectSite(api: StatorApi, seed: Seed, withSecret = false): Promise<void> {
  await api.DELETE("/armature/connection");
  must(
    await api.PUT("/armature/connection", {
      body: { baseUrl: seed.url, ...(withSecret ? { webhookSecret: seed.webhookSecret } : {}) },
    }),
  );
}

/** Stores a person's Armature token as the Stator member api acts for. */
export async function connectAccount(api: StatorApi, person: ArmaturePerson): Promise<void> {
  must(await api.PUT("/armature/account/token", { body: { token: person.token } }));
}
