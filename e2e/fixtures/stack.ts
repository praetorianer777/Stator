import { existsSync, readFileSync } from "node:fs";
import { dirname, resolve } from "node:path";
import { fileURLToPath } from "node:url";

/** Where `make stack-up` writes the ports and project name of this checkout's stack. */
export const STACK_ENV_FILE = resolve(dirname(fileURLToPath(import.meta.url)), "../../.cache/stack.env");

function readStackEnv(): Record<string, string> {
  if (!existsSync(STACK_ENV_FILE)) return {};
  const out: Record<string, string> = {};
  for (const line of readFileSync(STACK_ENV_FILE, "utf8").split("\n")) {
    const match = /^([A-Z0-9_]+)=(.*)$/.exec(line.trim());
    if (match) out[match[1]!] = match[2]!;
  }
  return out;
}

const env = readStackEnv();

// The environment wins, so the suite can be pointed at a stack elsewhere.
function setting(name: string): string {
  const value = process.env[name] ?? env[name];
  if (!value) {
    throw new Error(`${name} is not set and ${STACK_ENV_FILE} does not name it. Start the stack with make up or make stack-up first.`);
  }
  return value;
}

/** The web client's origin, which proxies /api/ to the api. */
export const WEB_URL = setting("STATOR_WEB_URL");

/** Keycloak as the browser sees it. */
export const KEYCLOAK_URL = setting("STATOR_KEYCLOAK_URL");

/** Mailpit's web and API, which catch every mail the stack sends; read when first needed. */
export function mailpitURL(): string {
  return setting("STATOR_MAILPIT_URL");
}

/** The secret the api's test endpoints ask for; read when first needed, so a stack without them still runs the rest. */
export function testEndpointsToken(): string {
  return setting("STATOR_TEST_ENDPOINTS_TOKEN");
}

/** The armature-stub as a browser opens it, which is the address an administrator connects; read when first needed. */
export function armatureURL(): string {
  return setting("STATOR_ARMATURE_URL");
}

/** The api as the armature-stub reaches it on the stack's network, where its webhooks go. */
export function apiFromStubURL(): string {
  return process.env.STATOR_API_FROM_STUB_URL ?? "http://api:8080";
}

/** The stack's database as its superuser, for arranging what no endpoint makes; read when first needed. */
export function superuserURL(): string {
  return setting("STATOR_TEST_SUPERUSER_URL");
}
