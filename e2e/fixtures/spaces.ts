import type { TestInfo } from "@playwright/test";
import type { components } from "../../web/src/api/schema";
import { must, type StatorApi } from "./api";

export type Space = components["schemas"]["Space"];

// A key is two to ten capital letters or digits, starting with a letter.
const KEY_MAX_LENGTH = 10;
const KEY_PREFIX = "E";

/** A space key no other test of the spec file, worker or earlier run has used, since they share an organisation. */
export function uniqueKey(testInfo: TestInfo): string {
  const stamp = `${testInfo.workerIndex.toString(36)}${Date.now().toString(36)}${testInfo.repeatEachIndex.toString(36)}`;
  return (KEY_PREFIX + stamp.toUpperCase()).slice(0, KEY_MAX_LENGTH);
}

export async function createSpace(api: StatorApi, key: string, name: string): Promise<Space> {
  return must(await api.POST("/spaces", { body: { key, name } })).space;
}

/** Deletes a space and everything in it; one that is gone already is fine. */
export async function deleteSpace(api: StatorApi, key: string): Promise<void> {
  await api.DELETE("/spaces/{spaceKey}", { params: { path: { spaceKey: key } } });
}
