import type { Page } from "@playwright/test";
import { must } from "../fixtures/api";
import { expect } from "../fixtures/auth";
import { caretTo } from "../fixtures/editor";
import { orgTest as test } from "../fixtures/org";
import { ONE_LOOK, openShowing } from "../fixtures/replica";
import { uniqueName } from "../fixtures/seed";
import { createPage, createSpace, deleteSpace, uniqueKey } from "../fixtures/spaces";

const doc = (...lines: string[]) => ({ type: "doc", content: lines.map((text) => ({ type: "paragraph", content: [{ type: "text", text }] })) });
// A live page's reader asks for it again every 3 seconds (LIVE_PAGE_REFRESH_MS
// in the web client), which is what brings the words without a reload; a
// replica short of the save is answered by the next ask.
const FOLLOWED = { timeout: 2 * 3_000 + ONE_LOOK.timeout } as const;

async function typeAtEnd(page: Page, text: string) {
  await caretTo(page.locator("#page-body"), "end");
  await page.keyboard.type(text);
}

test.describe("live pages", { tag: ["@auth", "@desktop"] }, () => {
  const made: string[] = [];
  test.afterEach(async ({ api }) => {
    for (const key of made.splice(0)) await deleteSpace(api, key);
  });

  test("an editor makes a page live, what they type reaches a reader without a reload, and the history keeps the session as one version", async ({
    page,
    api,
    pageAs,
  }, testInfo) => {
    test.slow();
    const key = uniqueKey(testInfo);
    made.push(key);
    const space = await createSpace(api, key, uniqueName(testInfo, "Live"));
    const notes = await createPage(api, space.homePageId, "Notes", doc("First words."));
    const path = `/s/${space.key}/p/${notes.id}/notes`;

    // A draft nobody published is named before going live throws it away.
    await page.goto(`${path}/edit`);
    await typeAtEnd(page, " Drafted.");
    await expect(page.locator("[data-draft-status]")).toHaveAttribute("data-draft-status", "saved");
    await page.locator('[data-action="close-editor"]').click();
    await expect(page.locator("[data-draft-note]")).toBeVisible();
    await page.locator('[data-action="page-menu"]').click();
    await page.getByRole("menuitem", { name: "Editing mode" }).click();
    const dialog = page.getByRole("dialog", { name: "How this page is edited" });
    await dialog.getByRole("radio", { name: /^Live/ }).check();
    await dialog.locator('[data-action="save-mode"]').click();
    await expect(dialog.locator("[data-drafts-pending]")).toContainText("has a draft of this page that nobody published");
    await dialog.locator('[data-action="discard-drafts-go-live"]').click();
    await expect(dialog).toBeHidden();
    await expect(page.locator("main [data-live-badge]")).toBeVisible();
    await expect(page.locator("[data-draft-note]")).toHaveCount(0);

    // Bob reads the page as it stands.
    const bob = await pageAs("bob");
    await openShowing(bob, path, bob.locator("main [data-live-badge]"));
    await expect(bob.locator("[data-doc]")).toContainText("First words.");
    await expect(bob.locator("[data-doc]")).not.toContainText("Drafted.");

    // Alice types; there is nothing to publish, and bob's open page follows.
    await page.goto(`${path}/edit`);
    await expect(page.locator("[data-page-editor]")).toHaveAttribute("data-page-mode", "live");
    await expect(page.locator('[data-action="publish"]')).toHaveCount(0);
    await expect(page.locator("#page-body")).toContainText("First words.");
    await typeAtEnd(page, " Typed live.");
    await expect(page.locator("[data-draft-status]")).toHaveAttribute("data-draft-status", "saved");
    await expect(bob.locator("[data-doc]")).toContainText("First words. Typed live.", FOLLOWED);
    await typeAtEnd(page, " And more.");
    await expect(page.locator("[data-draft-status]")).toHaveAttribute("data-draft-status", "saved");
    await expect(bob.locator("[data-doc]")).toContainText("Typed live. And more.", FOLLOWED);

    await page.locator('[data-action="close-editor"]').click();
    await expect(page.locator("[data-doc]")).toContainText("First words. Typed live. And more.");

    // Both saves went into one version, read as alice's API client, another session.
    await expect
      .poll(async () => {
        const { versions } = must(await api.GET("/pages/{pageID}/versions", { params: { path: { pageID: notes.id } } }));
        return versions.map((each) => `${each.number}${each.live ? " live" : ""}`);
      })
      .toEqual(["2 live", "1"]);
    await page.goto(`${path}/history`);
    await expect(page.locator("[data-live-version]")).toHaveCount(1);
  });
});
