import type { Page } from "@playwright/test";
import { must } from "../fixtures/api";
import { expect } from "../fixtures/auth";
import { caretTo } from "../fixtures/editor";
import { orgTest as test } from "../fixtures/org";
import { ONE_LOOK, openEditor, openShowing, openUntil } from "../fixtures/replica";
import { uniqueName } from "../fixtures/seed";
import { expectAccessible } from "../fixtures/shell";
import { createPage, createSpace, deleteSpace, uniqueKey } from "../fixtures/spaces";

const doc = (...lines: string[]) => ({ type: "doc" as const, content: lines.map((text) => ({ type: "paragraph", content: [{ type: "text", text }] })) });
const banner = (page: Page) => page.locator("[data-review-banner]");

test.describe("page reviews", { tag: ["@auth", "@desktop"] }, () => {
  const made: string[] = [];
  test.afterEach(async ({ api }) => {
    for (const key of made.splice(0)) await deleteSpace(api, key);
  });

  test("alice requires a review, bob's draft waits, alice approves, the page goes out, and its readers confirm they read it", async ({
    page,
    api,
    apiAs,
    pageAs,
  }, testInfo) => {
    test.slow();
    const key = uniqueKey(testInfo);
    made.push(key);
    const space = await createSpace(api, key, uniqueName(testInfo, "Policies"));
    const policy = await createPage(api, space.homePageId, "Travel policy", doc("Book the train."));
    const path = `/s/${space.key}/p/${policy.id}/travel-policy`;
    const aliceId = String(must(await api.GET("/auth/me")).user.id);
    must(
      await api.PUT("/spaces/{spaceKey}/permissions", {
        params: { path: { spaceKey: space.key } },
        body: {
          grants: [
            { subject: { type: "everyone" }, permissions: ["view", "addPages", "addComments"] },
            { subject: { type: "user", id: aliceId }, permissions: ["administer"] },
          ],
        },
      }),
    );

    // Alice holds the page to her own review.
    must(
      await api.PUT("/pages/{pageID}/review-rule", {
        params: { path: { pageID: policy.id } },
        body: { reviewers: [{ type: "user", id: aliceId }], required: 1 },
      }),
    );
    await openShowing(page, path, page.getByRole("heading", { level: 1, name: "Travel policy" }));
    await page.locator('[data-action="page-menu"]').click();
    await page.locator('[data-action="review-rule"]').click();
    await expect(page.locator("[data-review-rule-dialog] [data-review-held-by]")).toHaveAttribute("data-review-held-by", "own");
    await page.keyboard.press("Escape");

    // Bob's draft goes to review instead of out.
    const bob = await pageAs("bob");
    await openEditor(bob, path, "Book the train.");
    await caretTo(bob.locator("#page-body"), "end");
    await bob.keyboard.type(" Fly only beyond 800 km.");
    await expect(bob.locator("[data-draft-status]")).toHaveAttribute("data-draft-status", "saved");
    await expect(bob.locator('[data-action="publish"]')).toHaveCount(0);
    await bob.locator('[data-action="submit-review"]').click();
    await bob.locator('[data-action="confirm-submit-review"]').click();
    await bob.waitForURL((url) => !url.pathname.endsWith("/edit"));
    await expect(banner(bob).locator("[data-review-approvals]")).toHaveAttribute("data-review-approvals", "0/1");
    await expect(bob.locator("[data-doc]").first()).not.toContainText("Fly only");
    await expect(banner(bob).locator('[data-action="open-review"]')).toHaveCount(0);

    // It waits on alice, who reviews the change and approves it.
    await openUntil(page, "/reviews", () => expect(page.locator("[data-waiting-review]")).toHaveCount(1, ONE_LOOK));
    await expect(page.locator('[data-action="waiting-reviews"] [data-waiting-badge]')).toHaveAttribute("data-waiting-badge", "1");
    await expectAccessible(page);
    await page.locator("[data-waiting-review]").getByRole("link", { name: "Travel policy" }).click();
    await banner(page).locator('[data-action="open-review"]').click();
    const review = page.locator("[data-review-dialog]");
    await expect(review.locator("[data-review-comparison]")).toContainText("Fly only beyond 800 km.");
    await expectAccessible(page);
    await review.locator('[data-action="approve-review"]').click();
    await expect(page.locator("[data-doc]").first()).toContainText("Book the train. Fly only beyond 800 km.");
    await expect(banner(page)).toHaveCount(0);
    const versions = must(await api.GET("/pages/{pageID}/versions", { params: { path: { pageID: policy.id } } })).versions;
    expect(versions[0]?.authorName).toBe(must(await (await apiAs("bob")).GET("/auth/me")).user.name);
    expect(versions[0]?.review?.approvedBy).toEqual([must(await api.GET("/auth/me")).user.name]);

    // Alice asks the page's readers to confirm, and bob does.
    await page.locator('[data-action="page-menu"]').click();
    await page.locator('[data-action="ask-read"]').click();
    await openUntil(bob, path, () => expect(bob.locator('[data-read-ask-banner="ask"]')).toBeVisible(ONE_LOOK));
    await expect(bob.locator("[data-doc]").first()).toContainText("Fly only beyond 800 km.");
    await bob.locator('[data-action="confirm-read"]').click();
    await expect(bob.locator('[data-read-ask-banner="confirmed"]')).toBeVisible();
    await openUntil(page, path, () => expect(page.locator('[data-action="read-by"]')).toHaveAttribute("data-read-by", "1", ONE_LOOK));
  });
});
