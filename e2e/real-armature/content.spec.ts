import type { Page, TestInfo } from "@playwright/test";
import { must, type StatorApi } from "../fixtures/api";
import { expect } from "../fixtures/auth";
import { caretTo } from "../fixtures/editor";
import { openPage, openShowing } from "../fixtures/replica";
import { uniqueName } from "../fixtures/seed";
import { createPage, createSpace, deleteSpace, publishFromEditor, uniqueKey } from "../fixtures/spaces";
import { teamTest as test } from "../fixtures/team";
import { connectAccount, connectSite, readSeed, type Seed } from "./armature";

// Every block that draws Armature's issues, and the chip, against the real
// Armature: alice reads with Mia's token, who works in the project, and bob
// with Nils's, who is in Armature but sees none of it.

type Body = Parameters<typeof createPage>[3];
const paragraph = (value: string) => ({ type: "paragraph", content: [{ type: "text", text: value }] });
const shown = (page: Page) => page.locator("main [data-doc]");

/** The seeded issues by key alone, so issues other specs file do not change what the blocks count. */
const seededQuery = (seed: Seed) => {
  const keys = Object.values(seed.issues).map((each) => each.key);
  return `key in (${keys.join(", ")}) ORDER BY key`;
};

function everything(seed: Seed, calendarId: string): Body {
  const query = seededQuery(seed);
  const project = seed.project.key;
  return {
    type: "doc",
    content: [
      {
        type: "paragraph",
        content: [
          { type: "text", text: "Sign-in is tracked in " },
          { type: "armatureIssue", attrs: { key: seed.issues.signIn.key } },
        ],
      },
      { type: "armatureIssueBlock", attrs: { key: seed.issues.guide.key } },
      { type: "armatureIssueList", attrs: { query, columns: ["key", "summary", "status", "assignee"], limit: 20 } },
      { type: "armatureChart", attrs: { project, query, chart: "pie", groupBy: "statusCategory", days: 30 } },
      { type: "armatureRoadmap", attrs: { project, query, groupBy: "epic" } },
      { type: "calendar", attrs: { calendarId, project } },
    ],
  } as Body;
}

test.describe("Armature's issues on a page, from a real Armature", { tag: ["@auth"] }, () => {
  const made: string[] = [];

  test.beforeEach(async ({ api }) => {
    const seed = readSeed();
    await connectSite(api, seed);
    await connectAccount(api, seed.people.member);
  });

  test.afterEach(async ({ api }) => {
    for (const key of made.splice(0)) await deleteSpace(api, key);
    await api.DELETE("/armature/connection");
  });

  async function freshSpace(api: StatorApi, testInfo: TestInfo) {
    const key = uniqueKey(testInfo);
    made.push(key);
    return createSpace(api, key, uniqueName(testInfo, "Real"));
  }

  test("each block and the chip show the issues Mia may see, and Nils sees none of them", async ({ page, api, apiAs, pageAs }, testInfo) => {
    const seed = readSeed();
    const { guide, runbooks, signIn, epic } = seed.issues;
    const space = await freshSpace(api, testInfo);
    const calendar = must(await api.POST("/spaces/{spaceKey}/calendars", { params: { path: { spaceKey: space.key } }, body: { name: "Team" } })).calendar;
    const target = await createPage(api, space.homePageId, uniqueName(testInfo, "Wiki launch"), everything(seed, calendar.id));

    await openPage(page, space.key, target);
    const chip = shown(page).locator(`[data-armature-issue="${signIn.key}"]`);
    await expect(chip).toHaveAttribute("data-state", "issue");
    await expect(chip).toContainText(signIn.summary);

    const block = shown(page).locator(`[data-armature-issue-block="${guide.key}"]`);
    await expect(block).toHaveAttribute("data-state", "issue");
    await expect(block).toContainText(guide.summary);
    await expect(block.locator('[data-field="assignee"]')).toContainText(seed.people.member.name);
    await expect(block.locator('[data-field="reporter"]')).toContainText(seed.people.admin.name);
    await expect(block.locator('[data-field="priority"]')).toContainText("High");
    await expect(block.getByRole("link", { name: `Open in Armature ${guide.key}` })).toHaveAttribute("href", `${seed.url}/issues/${guide.key}`);

    const list = shown(page).locator("[data-armature-issue-list]");
    await expect(list.locator("[data-issue-row]")).toHaveCount(4);
    await expect(list.locator("[data-issue-row]").first()).toContainText(epic.summary);
    await expect(list).toContainText(seed.people.member.name);

    const chart = shown(page).locator('[data-armature-chart="pie"]');
    await expect(chart).toHaveAttribute("data-state", "chart");
    await expect(chart.getByRole("table", { name: "Issues by status category" }).getByRole("row", { name: /To do/ })).toContainText("4");

    const roadmap = shown(page).locator('[data-armature-roadmap="epic"]');
    await expect(roadmap).toHaveAttribute("data-state", "roadmap");
    await expect(roadmap.getByRole("region", { name: epic.summary }).locator("[data-roadmap-row]")).toHaveText([
      new RegExp(guide.key),
      new RegExp(runbooks.key),
    ]);

    const month = shown(page).getByRole("figure", { name: "Team" });
    await expect(month).toHaveAttribute("data-state", "month");
    await expect(month.locator(`[data-day="${seed.dates.signInDue}"] [data-calendar-issue="${signIn.key}"]`)).toBeVisible();

    // Nils's token works, and Armature shows him nothing of the project.
    const bobApi = await apiAs("bob");
    await connectAccount(bobApi, seed.people.outsider);
    const bob = await pageAs("bob");
    await openShowing(bob, `/s/${space.key}/p/${target.id}/page`, "Sign-in is tracked in");
    for (const hidden of [shown(bob).locator(`[data-armature-issue="${signIn.key}"]`), shown(bob).locator(`[data-armature-issue-block="${guide.key}"]`)]) {
      await expect(hidden).toHaveAttribute("data-state", "hidden");
      await expect(hidden).toContainText("Not available");
    }
    await expect(shown(bob).locator("[data-armature-issue-list]")).toHaveAttribute("data-state", "empty");
    await expect(shown(bob).locator('[data-armature-chart="pie"]')).toHaveAttribute("data-state", "no_project");
    await expect(shown(bob).locator('[data-armature-roadmap="epic"]')).toHaveAttribute("data-state", "no_project");
    await expect(shown(bob).locator("[data-calendar-issue]")).toHaveCount(0);
    for (const issue of [guide, runbooks, signIn, epic]) await expect(bob.locator("main")).not.toContainText(issue.summary);
  });

  test("Mia finds an issue by a word of its summary, and Nils finds nothing", async ({ page, api, apiAs }, testInfo) => {
    const seed = readSeed();
    const { guide } = seed.issues;
    const word = "onboarding";
    expect(guide.summary.toLowerCase()).toContain(word);
    const space = await freshSpace(api, testInfo);
    const target = await createPage(api, space.homePageId, uniqueName(testInfo, "Onboarding"), { type: "doc", content: [paragraph("Tracked work.")] } as Body);

    await openPage(page, space.key, target);
    await page.locator('[data-action="edit-page"]').click();
    const box = page.locator("#page-body");
    await caretTo(box, "end");
    await page.keyboard.type(` See [[${word}`);
    const option = page.locator(`[data-issue-menu] [data-issue-option="${guide.key}"]`);
    await expect(option).toContainText(guide.summary);
    await expect(option).toContainText(seed.project.key);
    await page.keyboard.press("Enter");
    await expect(box.locator(`[data-armature-issue="${guide.key}"]`)).toBeVisible();
    await publishFromEditor(page);
    await expect(shown(page).locator(`[data-armature-issue="${guide.key}"]`)).toHaveAttribute("data-state", "issue");

    const found = must(await api.GET("/armature/find", { params: { query: { words: word } } }));
    expect(found.status).toBe("ok");
    expect(found.issues.map((each) => each.key)).toContain(guide.key);

    // Nils's token works, and Armature's search shows him nothing of the project.
    const bobApi = await apiAs("bob");
    await connectAccount(bobApi, seed.people.outsider);
    const none = must(await bobApi.GET("/armature/find", { params: { query: { words: word } } }));
    expect(none.status).toBe("ok");
    expect(none.issues).toEqual([]);
  });

  test("alice inserts an issue from the slash menu by its address in Armature", async ({ page, api }, testInfo) => {
    const seed = readSeed();
    const { runbooks } = seed.issues;
    const space = await freshSpace(api, testInfo);
    const target = await createPage(api, space.homePageId, uniqueName(testInfo, "Runbooks"), { type: "doc", content: [paragraph("Tracked work.")] } as Body);

    await openPage(page, space.key, target);
    await page.locator('[data-action="edit-page"]').click();
    const box = page.locator("#page-body");
    await caretTo(box, "end");
    await page.keyboard.press("Enter");
    await page.keyboard.type("/ticket");
    await expect(page.locator('[data-slash-item="armatureIssue"]')).toBeVisible();
    await page.keyboard.press("Enter");

    const dialog = page.getByRole("dialog", { name: "Insert an Armature issue" });
    const field = dialog.getByRole("combobox", { name: "Find an issue" });
    await field.fill(`${seed.project.key}-999`);
    await expect(dialog).toContainText(`No issue ${seed.project.key}-999 that you can see in Armature.`);
    await field.fill(`${seed.url}/issues/${runbooks.key.toLowerCase()}`);
    await expect(dialog.locator(`[data-issue-option="${runbooks.key}"]`)).toContainText(runbooks.summary);
    await page.keyboard.press("Enter");
    await expect(dialog).toBeHidden();
    await publishFromEditor(page);

    const block = shown(page).locator(`[data-armature-issue-block="${runbooks.key}"]`);
    await expect(block).toHaveAttribute("data-state", "issue");
    await expect(block.locator('[data-field="due"]')).toBeVisible();
  });
});
