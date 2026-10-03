import type { Page, TestInfo } from "@playwright/test";
import { must, type StatorApi } from "../fixtures/api";
import { expect } from "../fixtures/auth";
import { caretTo } from "../fixtures/editor";
import { orgTest as test } from "../fixtures/org";
import { uniqueName } from "../fixtures/seed";
import { expectAccessible, startInScheme, type ColourScheme } from "../fixtures/shell";
import { createPage, createSpace, deleteSpace, publishFromEditor, uniqueKey, type Space } from "../fixtures/spaces";

const BOB = "Bob Builder";
// A due day is read in UTC, as the server reads it.
const TODAY = new Date().toISOString().slice(0, 10);
const LONG_AGO = "2020-01-06";

const editorBox = (page: Page) => page.locator("#page-body");
const shown = (page: Page) => page.locator("main [data-doc]");
const reported = (scope: ReturnType<Page["locator"]>) => scope.locator("[data-reported-task]");

type Inline = Record<string, unknown>;
const words = (text: string): Inline => ({ type: "text", text });
const mention = (id: string, label: string): Inline => ({ type: "mention", attrs: { id, label } });
const day = (date: string): Inline => ({ type: "date", attrs: { date } });

/** A page of one paragraph and a checklist, each item its words and whatever else it holds. */
function checklist(lead: string, ...items: Inline[][]) {
  return {
    type: "doc" as const,
    content: [
      { type: "paragraph", content: [words(lead)] },
      {
        type: "taskList",
        content: items.map((inline) => ({ type: "taskItem", attrs: { checked: false }, content: [{ type: "paragraph", content: inline }] })),
      },
    ],
  };
}

/** Opens a page until it holds the words given, which a replica may lag behind on. */
async function open(page: Page, path: string, text: string): Promise<void> {
  await expect(async () => {
    await page.goto(path);
    await expect(page.locator("main")).toContainText(text, { timeout: 2_000 });
  }).toPass();
}

test.describe("task report", { tag: ["@auth"] }, () => {
  const made: string[] = [];
  test.afterEach(async ({ api }) => {
    for (const key of made.splice(0)) await deleteSpace(api, key);
  });

  async function freshSpace(api: StatorApi, testInfo: TestInfo, title: string): Promise<Space> {
    const key = uniqueKey(testInfo);
    made.push(key);
    return createSpace(api, key, uniqueName(testInfo, title));
  }

  test("a lead reports a space's open tasks, narrows the report to one person, and a tick takes a task off it", async ({ page, api, apiAs }, testInfo) => {
    const space = await freshSpace(api, testInfo, "Release");
    const bobId = must(await (await apiAs("bob")).GET("/auth/me")).user.id;
    await createPage(
      api,
      space.homePageId,
      "Plan",
      checklist("The plan.", [words("Ship the release "), mention(bobId, BOB), words(" by "), day(TODAY)], [words("Write the notes by "), day(LONG_AGO)]),
    );
    const status = await createPage(api, space.homePageId, "Status", { type: "doc", content: [{ type: "paragraph", content: [words("Where we are.")] }] });
    const path = `/s/${space.key}/p/${status.id}/status`;

    await open(page, `${path}/edit`, "Where we are.");
    await caretTo(editorBox(page), "end");
    await page.keyboard.press("Enter");
    await page.keyboard.type("/overdue");
    await expect(page.getByRole("listbox", { name: "Insert a block" })).toBeVisible();
    await page.keyboard.press("Enter");
    const dialog = page.getByRole("dialog", { name: "Insert a task report" });
    await dialog.getByLabel("Space").selectOption(space.key);
    await dialog.getByRole("button", { name: "Insert" }).click();
    const inEditor = editorBox(page).locator("[data-task-report]");
    // Overdue first, then by day.
    await expect(reported(inEditor)).toHaveText([/Write the notes/, /Ship the release/]);

    await editorBox(page).locator("[data-task-report-node]").getByRole("button", { name: "Edit report" }).click();
    const edit = page.getByRole("dialog", { name: "Edit the task report" });
    await edit.getByLabel("Assigned to").selectOption({ label: BOB });
    await edit.getByRole("button", { name: "Save" }).click();
    await expect(editorBox(page).getByRole("region", { name: `Open tasks in ${space.key}, assigned to ${BOB}` })).toBeVisible();
    await expect(reported(inEditor)).toHaveText([/Ship the release/]);
    await expect(page.locator("[data-draft-status]")).toHaveAttribute("data-draft-status", "saved");
    await publishFromEditor(page);

    const report = shown(page).getByRole("region", { name: `Open tasks in ${space.key}, assigned to ${BOB}` });
    const row = reported(report);
    await expect(row).toHaveCount(1);
    await expect(row.locator("[data-task-due]")).toHaveText("Due today");
    await expect(row).toContainText(BOB);
    // Alice may edit the plan, so she ticks bob's task off from the report.
    await row.getByRole("checkbox").click();
    await expect(report.getByText("No task on a page you can read matches this report.")).toBeVisible();
    const done = must(await api.GET("/task-report", { params: { query: { space: space.key, state: "done" } } }));
    expect(done.tasks.map((task) => task.text)).toEqual([`Ship the release @${BOB} by ${TODAY}`]);
  });

  for (const scheme of ["light", "dark"] as ColourScheme[]) {
    test(`a task report passes axe in ${scheme}`, async ({ page, api }, testInfo) => {
      const space = await freshSpace(api, testInfo, "Axe");
      await createPage(api, space.homePageId, "Plan", checklist("Plan.", [words("Overdue thing "), day(LONG_AGO)], [words("Undated thing")]));
      const overview = await createPage(api, space.homePageId, "Reports", {
        type: "doc",
        content: [
          { type: "taskReport", attrs: { space: space.key, assignee: null, due: "any", state: "open", limit: 20 } },
          { type: "taskReport", attrs: { space: space.key, assignee: "none", due: "today", state: "done", limit: 10 } },
        ],
      });
      await startInScheme(page, scheme);
      await open(page, `/s/${space.key}/p/${overview.id}/reports`, "Overdue thing");
      await expect(shown(page).locator('[data-task-report][data-state="empty"]')).toHaveCount(1);
      await expect(shown(page).locator("[data-task-due]")).toHaveText([/Overdue/]);
      await page.screenshot({ path: testInfo.outputPath(`task-report-${scheme}.png`), fullPage: true });
      await expectAccessible(page);
    });
  }
});
