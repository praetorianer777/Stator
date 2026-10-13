import { request, type APIRequestContext } from "@playwright/test";
import { settleReplicas } from "./db";
import { testEndpointsToken, WEB_URL } from "./stack";

/** Where the api's test endpoints are, which the compose stack switches on and no deployment does. */
export const TEST_API_PATH = "/api/v1/test";
const TEST_TOKEN_HEADER = "X-Stator-Test-Token";

/** A request context that carries the test endpoints' token and no session; dispose it when done. */
export async function testEndpoints(): Promise<APIRequestContext> {
  return request.newContext({ baseURL: WEB_URL, extraHTTPHeaders: { [TEST_TOKEN_HEADER]: testEndpointsToken() } });
}

/**
 * Puts the organization's settings back as the bootstrap left them, and waits
 * until the replicas have it: the next reads come from sessions that wrote nothing.
 */
export async function resetInstance(): Promise<void> {
  const context = await testEndpoints();
  try {
    const res = await context.post(`${TEST_API_PATH}/reset`);
    if (res.status() !== 204) throw new Error(`POST ${TEST_API_PATH}/reset answered ${res.status()}: ${await res.text()}`);
  } finally {
    await context.dispose();
  }
  await settleReplicas();
}

/**
 * Makes an example space one made before its content had versions, without
 * the page of the file named, and waits until the replicas have it.
 */
export async function ageExample(spaceKey: string, drop: string): Promise<void> {
  const context = await testEndpoints();
  try {
    const path = `${TEST_API_PATH}/spaces/${encodeURIComponent(spaceKey)}/example-space/age`;
    const res = await context.post(path, { data: { drop } });
    if (res.status() !== 204) throw new Error(`POST ${path} answered ${res.status()}: ${await res.text()}`);
  } finally {
    await context.dispose();
  }
  await settleReplicas();
}
