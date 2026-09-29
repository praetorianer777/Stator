import type { Page as PwPage, TestInfo } from "@playwright/test";
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

export type Page = components["schemas"]["Page"];

type Body = components["schemas"]["PageCreateInput"]["body"];

/** Adds a page under a parent, last among its children, published as version 1 so everybody sees it. */
export async function createPage(api: StatorApi, parentId: string, title: string, body?: Body): Promise<Page> {
  return must(await api.POST("/pages", { body: { parentId, title, body, publish: true } })).page;
}

/** Publishes what the open editor holds, with a comment when one is given, and waits to be back on the page. */
export async function publishFromEditor(page: PwPage, comment = ""): Promise<void> {
  await page.locator('[data-action="publish"]').click();
  const dialog = page.locator("[data-publish-dialog]");
  if (comment) await dialog.getByLabel("What changed", { exact: true }).fill(comment);
  await dialog.locator('[data-action="confirm-publish"]').click();
  await page.locator("[data-page]").waitFor();
}

/** The titles directly under a parent of a space, in order. */
export async function childTitles(api: StatorApi, key: string, parentId: string): Promise<string[]> {
  const { pages } = must(await api.GET("/spaces/{spaceKey}/pages", { params: { path: { spaceKey: key }, query: { parent: parentId } } }));
  return pages.map((each) => each.title);
}
