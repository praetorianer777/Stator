import type { Page } from "@playwright/test";
import { must, type StatorApi } from "../fixtures/api";
import { expect } from "../fixtures/auth";
import { bringScheduleDue } from "../fixtures/db";
import { caretTo } from "../fixtures/editor";
import { orgTest as test } from "../fixtures/org";
import { ONE_LOOK, openPage, openShowing, openUntil } from "../fixtures/replica";
import { uniqueName } from "../fixtures/seed";
import { expectAccessible, scrollsSideways, startInScheme, type ColourScheme } from "../fixtures/shell";
import { createPage, createSpace, deleteSpace, uniqueKey, type Page as WikiPage } from "../fixtures/spaces";

const doc = (...lines: string[]) => ({ type: "doc" as const, content: lines.map((text) => ({ type: "paragraph", content: [{ type: "text", text }] })) });
const note = (page: Page) => page.locator("[data-schedule-note]");
const bell = (page: Page) => page.locator('[data-action="notifications"]');
const panel = (page: Page) => page.locator("[data-notification-panel]");

/** Drafts new words and schedules them an hour ahead through the API, as alice. */
async function scheduleAsAlice(api: StatorApi, target: WikiPage, text: string) {
  must(
    await api.PUT("/pages/{pageID}/draft", {
      params: { path: { pageID: target.id } },
      body: { title: target.title, body: doc(text), baseVersion: target.version },
    }),
  );
  const publishAt = new Date(Date.now() + 60 * 60 * 1000).toISOString();
  must(await api.PUT("/pages/{pageID}/schedule", { params: { path: { pageID: target.id } }, body: { publishAt, notifyWatchers: true } }));
}

test.describe("scheduled publishing", { tag: ["@auth"] }, () => {
  const made: string[] = [];
  test.afterEach(async ({ api }) => {
    for (const key of made.splice(0)) await deleteSpace(api, key);
  });

  async function freshPage(api: StatorApi, testInfo: Parameters<typeof uniqueKey>[0], label: string) {
    const key = uniqueKey(testInfo);
    made.push(key);
    const space = await createSpace(api, key, uniqueName(testInfo, label));
    const target = await createPage(api, space.homePageId, "Announcement", doc("Coming soon."));
    return { space, target, path: `/s/${space.key}/p/${target.id}/announcement` };
  }

  test(
    "alice schedules her draft, keeps editing, and at its time it goes out in her name and bob, who watches, hears",
    { tag: ["@desktop"] },
    async ({ page, api, apiAs, pageAs }, testInfo) => {
      const { space, target, path } = await freshPage(api, testInfo, "Schedule");
      must(await (await apiAs("bob")).PUT("/pages/{pageID}/watch", { params: { path: { pageID: target.id } }, body: {} }));

      await page.goto(`${path}/edit`);
      await caretTo(page.locator("#page-body"), "end");
      await page.keyboard.type(" Launching on Monday.");
      await expect(page.locator("[data-draft-status]")).toHaveAttribute("data-draft-status", "saved");
      await page.locator('[data-action="publish"]').click();
      const dialog = page.locator("[data-publish-dialog]");
      await dialog.getByRole("radio", { name: "At a set time" }).check();
      await expect(dialog.getByLabel("Publish at")).toHaveValue(/^\d{4}-\d{2}-\d{2}T\d{2}:00$/);
      await expect(dialog.getByText(/^In your time zone, /)).toBeVisible();
      await dialog.getByLabel("What changed", { exact: true }).fill("Launch");
      await dialog.locator('[data-action="confirm-schedule"]').click();
      await page.waitForURL((url) => !url.pathname.endsWith("/edit"));

      // The page still reads as it did; alice is told when her draft goes out.
      await expect(note(page)).toHaveAttribute("data-schedule-note", "waiting");
      await expect(note(page)).toContainText("Your draft is scheduled to publish on");
      await expect(page.locator("[data-doc]")).not.toContainText("Launching on Monday.");

      // Until then the draft is hers to change, and what stands then goes out.
      await note(page).locator('[data-action="edit-schedule"]').click();
      await expect(page.locator("[data-schedule-tag]")).toContainText("Publishes");
      await caretTo(page.locator("#page-body"), "end");
      await page.keyboard.type(" Doors at nine.");
      await expect(page.locator("[data-draft-status]")).toHaveAttribute("data-draft-status", "saved");
      await page.locator('[data-action="close-editor"]').click();

      const bob = await pageAs("bob");
      await openPage(bob, space.key, target, () => expect(note(bob)).toContainText("scheduled their draft of this page", ONE_LOOK));
      await expect(bob.locator("[data-doc]")).not.toContainText("Launching on Monday.");

      await bringScheduleDue(target.id);
      await openShowing(bob, path, "Launching on Monday. Doors at nine.");
      await openUntil(page, path, () => expect(note(page)).toHaveCount(0, ONE_LOOK));
      await expect(page.locator("[data-draft-note]")).toHaveCount(0);

      await expect
        .poll(async () => {
          const { versions } = must(await api.GET("/pages/{pageID}/versions", { params: { path: { pageID: target.id } } }));
          return versions.map((each) => `${each.number} ${each.comment}`);
        })
        .toEqual(["2 Launch", "1 "]);
      const told = panel(bob).locator('[data-notification="published"]');
      await expect(async () => {
        await bob.goto("/");
        await bell(bob).click();
        await expect(told).toContainText("published version 2 of Announcement", ONE_LOOK);
      }).toPass();
    },
  );

  test("an editor sees whose publish is scheduled and calls it off, and the draft stays with its author", async ({ page, api, pageAs }, testInfo) => {
    const { target, path } = await freshPage(api, testInfo, "Call off");
    await scheduleAsAlice(api, target, "Alice's plan.");

    await openUntil(page, path, () => expect(note(page)).toHaveAttribute("data-schedule-note", "waiting", ONE_LOOK));
    expect(await scrollsSideways(page)).toBe(false);

    const bob = await pageAs("bob");
    await openUntil(bob, path, () => expect(note(bob)).toContainText("scheduled their draft of this page to publish on", ONE_LOOK));
    await expect(note(bob).locator('[data-action="edit-schedule"]')).toHaveCount(0);
    bob.once("dialog", (asked) => void asked.accept());
    await note(bob).locator('[data-action="cancel-schedule"]').click();
    await expect(note(bob)).toHaveCount(0);

    await openUntil(page, path, () => expect(page.locator("[data-draft-note]")).toBeVisible(ONE_LOOK));
    await expect(note(page)).toHaveCount(0);
  });

  for (const scheme of ["light", "dark"] as ColourScheme[]) {
    test(`the schedule's note and the publish dialog are accessible in ${scheme}`, async ({ page, api }, testInfo) => {
      const { target, path } = await freshPage(api, testInfo, `Axe ${scheme}`);
      await startInScheme(page, scheme);
      await scheduleAsAlice(api, target, "Words for later.");
      await openUntil(page, path, () => expect(note(page)).toBeVisible(ONE_LOOK));
      await expectAccessible(page);

      await note(page).locator('[data-action="edit-schedule"]').click();
      await expect(page.locator("#page-body")).toContainText("Words for later.");
      await page.locator('[data-action="publish"]').click();
      const dialog = page.locator("[data-publish-dialog]");
      await expect(dialog.getByRole("radio", { name: "At a set time" })).toBeChecked();
      await expect(dialog.getByLabel("Publish at")).toBeVisible();
      await expectAccessible(page);
      expect(await scrollsSideways(page)).toBe(false);
    });
  }
});
