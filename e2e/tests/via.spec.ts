import type { Page } from "@playwright/test";
import { must } from "../fixtures/api";
import { expect } from "../fixtures/auth";
import { teamTest as test } from "../fixtures/team";
import { openShowing } from "../fixtures/replica";
import { uniqueName } from "../fixtures/seed";
import { expectAccessible } from "../fixtures/shell";
import { createPage, createSpace, deleteSpace, uniqueKey } from "../fixtures/spaces";
import { WEB_URL } from "../fixtures/stack";

const TOKEN = "Nightly script";
// A token alice just made may still be on its way to the replica that
// authenticates it, so the script's first call is tried until it lands.
const REPLICA_CATCH_UP_MS = 10_000;

const version = (page: Page, n: number) => page.locator(`[data-version-row="${n}"]`);

// The space and the token are this spec's own, made by its own alice, so they meet no other spec.
test.describe("what a token wrote", { tag: ["@auth", "@desktop"] }, () => {
  const made: string[] = [];
  test.afterEach(async ({ api }) => {
    for (const key of made.splice(0)) await deleteSpace(api, key);
    for (const token of (await api.GET("/tokens")).data?.tokens ?? []) {
      await api.DELETE("/tokens/{tokenID}", { params: { path: { tokenID: token.id } } });
    }
  });

  test("a version published through a token says so, and through which, in the page's history", async ({ page, api }, testInfo) => {
    const key = uniqueKey(testInfo);
    made.push(key);
    const space = await createSpace(api, key, uniqueName(testInfo, "Through"));
    const plan = await createPage(api, space.homePageId, "Plan");
    const secret = must(await api.POST("/tokens", { body: { name: TOKEN } })).token.secret!;
    await expect(async () => {
      const changed = await fetch(`${WEB_URL}/api/v1/pages/${plan.id}`, {
        method: "PATCH",
        headers: { Authorization: `Bearer ${secret}`, "Content-Type": "application/json" },
        body: JSON.stringify({ title: "Plan, revised", version: 1 }),
      });
      expect(changed.status).toBe(200);
    }).toPass({ timeout: REPLICA_CATCH_UP_MS });

    await openShowing(page, `/s/${space.key}/p/${plan.id}/plan/history`, version(page, 2));
    await expect(version(page, 2).locator('[data-via="token"]')).toContainText(`through ${TOKEN}`);
    await expect(version(page, 1).locator("[data-via]")).toHaveCount(0);
    await expectAccessible(page);
  });
});
