import type { Page, TestInfo } from "@playwright/test";
import { must, type StatorApi } from "../fixtures/api";
import { expect } from "../fixtures/auth";
import { caretTo } from "../fixtures/editor";
import { orgTest as test } from "../fixtures/org";
import { ONE_LOOK, openPage, openUntil } from "../fixtures/replica";
import { uniqueName } from "../fixtures/seed";
import { expectAccessible, scrollsSideways, startInScheme, type ColourScheme } from "../fixtures/shell";
import { createPage, createSpace, deleteSpace, publishFromEditor, uniqueKey, type Space } from "../fixtures/spaces";

// Delivery goes through the outbox and the worker.
const DELIVERY_MS = 30_000;
const BOB = "Bob Builder";
// A due day is read in UTC, as the server reads it.
const TODAY = new Date().toISOString().slice(0, 10);
const LONG_AGO = "2020-01-06";

const editorBox = (page: Page) => page.locator("#page-body");
const bell = (page: Page) => page.locator('[data-action="notifications"]');
const panel = (page: Page) => page.locator("[data-notification-panel]");
const taskRow = (page: Page, words: string) => page.locator("[data-task-row]", { hasText: words });

type Inline = Record<string, unknown>;

/** A page of one paragraph and a checklist, each item its words and whatever else it holds. */
function checklist(lead: string, ...items: Inline[][]) {
  return {
    type: "doc" as const,
    content: [
      { type: "paragraph", content: [{ type: "text", text: lead }] },
      {
        type: "taskList",
        content: items.map((inline) => ({ type: "taskItem", attrs: { checked: false }, content: [{ type: "paragraph", content: inline }] })),
      },
    ],
  };
}

const words = (text: string): Inline => ({ type: "text", text });
const mention = (id: string, label: string): Inline => ({ type: "mention", attrs: { id, label } });
const day = (date: string): Inline => ({ type: "date", attrs: { date } });

/** Opens the list of one's tasks until a task shows up there, as a returning reader would. */
async function listed(page: Page, text: string) {
  await expect(async () => {
    await page.goto("/tasks");
    await expect(taskRow(page, text)).toBeVisible(ONE_LOOK);
  }).toPass({ timeout: DELIVERY_MS });
  return taskRow(page, text);
}

async function insert(page: Page, query: string) {
  await page.keyboard.type(`/${query}`);
  await expect(page.getByRole("listbox", { name: "Insert a block" })).toBeVisible();
  await page.keyboard.press("Enter");
}

async function bobsId(apiAs: (user: "bob") => Promise<StatorApi>) {
  return must(await (await apiAs("bob")).GET("/auth/me")).user.id;
}

test.describe("tasks", { tag: ["@auth"] }, () => {
  const made: string[] = [];
  test.afterEach(async ({ api }) => {
    for (const key of made.splice(0)) await deleteSpace(api, key);
  });

  async function freshSpace(api: StatorApi, testInfo: TestInfo, label: string): Promise<Space> {
    const key = uniqueKey(testInfo);
    made.push(key);
    return createSpace(api, key, uniqueName(testInfo, label));
  }

  test(
    "alice assigns bob a task with a day, bob is told, finds it in his tasks and ticks it off from the keyboard",
    { tag: ["@desktop"] },
    async ({ page, api, pageAs }, testInfo) => {
      test.slow();
      const space = await freshSpace(api, testInfo, "Sync");
      const minutes = await createPage(api, space.homePageId, uniqueName(testInfo, "Minutes"), {
        type: "doc",
        content: [{ type: "paragraph", content: [{ type: "text", text: "Weekly sync." }] }],
      });

      await openPage(page, space.key, minutes);
      await page.locator('[data-action="edit-page"]').click();
      await caretTo(editorBox(page), "end");
      await page.keyboard.press("Enter");
      await insert(page, "todo");
      await page.keyboard.type("Send the notes ");
      await page.keyboard.type("@bob");
      const people = page.getByRole("listbox", { name: "People to mention" });
      await expect(people.getByRole("option", { name: new RegExp(BOB) })).toBeVisible();
      await page.keyboard.press("Enter");
      await expect(people).toHaveCount(0);
      await page.keyboard.type(" by ");
      await insert(page, "date");
      const dateDialog = page.getByRole("dialog", { name: "Date" });
      await expect(dateDialog.getByLabel("Day")).toBeFocused();
      await dateDialog.getByLabel("Day").fill(TODAY);
      await page.keyboard.press("Enter");
      await expect(dateDialog).toBeHidden();
      await expect(editorBox(page).locator('ul[data-type="taskList"] [data-mention]')).toHaveText(`@${BOB}`);
      await expect(page.locator("[data-draft-status]")).toHaveAttribute("data-draft-status", "saved");
      await publishFromEditor(page);

      const item = page.locator("[data-doc] li[data-task-id]");
      await expect(item).toHaveCount(1);
      await expect(item.locator("[data-task-due]")).toHaveText("Due today");
      const text = `Send the notes @${BOB} by ${TODAY}`;

      const bob = await pageAs("bob");
      await bob.goto("/spaces");
      const told = panel(bob).locator('[data-notification="assigned"]', { hasText: minutes.title });
      await expect(async () => {
        await bob.reload();
        await bell(bob).click();
        await expect(told).toBeVisible(ONE_LOOK);
      }).toPass({ timeout: DELIVERY_MS });
      await expect(told).toContainText(`assigned you a task on ${minutes.title}`);
      await expect(told).toContainText("Send the notes");
      await bob.keyboard.press("Escape");

      await bob.locator('nav a[href="/tasks"]').first().click();
      await expect(bob).toHaveURL(/\/tasks$/);
      const row = await listed(bob, text);
      await expect(row.getByRole("link", { name: minutes.title })).toBeVisible();
      await expect(row.locator("[data-task-due]")).toHaveText("Due today");
      const box = row.getByRole("checkbox", { name: `Done: ${text}` });
      await box.focus();
      await bob.keyboard.press("Space");
      await expect(bob.getByRole("status").filter({ hasText: `${text} is done.` })).toBeAttached();
      await expect(taskRow(bob, text)).toHaveCount(0);

      // The tabs are one stop: the arrow moves to the done tasks.
      await bob.getByRole("tab", { name: "Open" }).focus();
      await bob.keyboard.press("ArrowRight");
      await expect(bob.getByRole("tab", { name: "Done" })).toHaveAttribute("aria-selected", "true");
      await expect(taskRow(bob, text).getByRole("checkbox")).toBeChecked();

      await expect(async () => {
        await page.reload();
        await expect(page.locator("[data-doc] li[data-task-id] input")).toBeChecked(ONE_LOOK);
      }).toPass();
      await expect(page.locator("[data-doc] li[data-task-id] [data-task-due]")).toHaveCount(0);
    },
  );

  test("a task on a page closed to its assignee leaves their list, and comes back with their access", async ({ api, apiAs, pageAs }, testInfo) => {
    test.slow();
    const space = await freshSpace(api, testInfo, "Closed");
    const bobId = await bobsId(apiAs);
    const aliceId = must(await api.GET("/auth/me")).user.id;
    const plans = await createPage(
      api,
      space.homePageId,
      uniqueName(testInfo, "Plans"),
      checklist("Plans.", [words("Draft the budget "), mention(bobId, BOB)]),
    );
    const text = `Draft the budget @${BOB}`;

    const bob = await pageAs("bob");
    const row = await listed(bob, text);
    await expect(row.getByRole("link", { name: plans.title })).toBeVisible();

    const restrict = (view: string[]) =>
      api.PUT("/pages/{pageID}/restrictions", {
        params: { path: { pageID: plans.id } },
        body: { view: view.map((id) => ({ type: "user" as const, id })), edit: [] },
      });
    must(await restrict([aliceId]));
    await openUntil(bob, "/tasks", async () => {
      await expect(bob.locator("[data-my-tasks] [role=tabpanel]")).toBeVisible(ONE_LOOK);
      await expect(bob.getByText("No open tasks.", { exact: false })).toBeVisible(ONE_LOOK);
    });
    await expect(taskRow(bob, text)).toHaveCount(0);

    must(await restrict([]));
    await listed(bob, text);
  });

  test(
    "a task's link opens the page at that task, inside a closed expand, from My tasks and from the notification",
    { tag: ["@desktop"] },
    async ({ api, apiAs, pageAs }, testInfo) => {
      test.slow();
      const space = await freshSpace(api, testInfo, "Anchored");
      const bobId = await bobsId(apiAs);
      const filler = Array.from({ length: 60 }, (_, i) => ({ type: "paragraph", content: [{ type: "text", text: `Minute ${i + 1} of a long meeting.` }] }));
      const text = `Send the notes @${BOB}`;
      const minutes = await createPage(api, space.homePageId, uniqueName(testInfo, "Long minutes"), {
        type: "doc",
        content: [
          ...filler,
          {
            type: "expand",
            attrs: { title: "Follow ups" },
            content: [
              {
                type: "taskList",
                content: [
                  { type: "taskItem", attrs: { checked: false }, content: [{ type: "paragraph", content: [words("Send the notes "), mention(bobId, BOB)] }] },
                ],
              },
            ],
          },
        ],
      });

      const bob = await pageAs("bob");
      const lands = async () => {
        const line = bob.locator("[data-doc] li[data-task-id]");
        await expect(bob.getByRole("button", { name: "Follow ups" })).toHaveAttribute("aria-expanded", "true");
        await expect(line).toBeInViewport();
        await expect(line).toBeFocused();
        await expect(line).toHaveAttribute("id", /^task-/);
      };

      const row = await listed(bob, text);
      await row.getByRole("link", { name: minutes.title }).click();
      await expect(bob).toHaveURL(/#task-[0-9a-f-]{36}$/);
      await lands();

      await bob.goto("/spaces");
      const told = panel(bob).locator('[data-notification="assigned"]', { hasText: minutes.title });
      await expect(async () => {
        await bob.reload();
        await bell(bob).click();
        await expect(told).toBeVisible(ONE_LOOK);
      }).toPass({ timeout: DELIVERY_MS });
      await told.click();
      await expect(bob).toHaveURL(/#task-[0-9a-f-]{36}$/);
      await lands();
    },
  );

  test("a link to a task the page no longer holds opens the page at its top and says so", async ({ api, pageAs }, testInfo) => {
    const space = await freshSpace(api, testInfo, "Gone");
    const plans = await createPage(api, space.homePageId, uniqueName(testInfo, "Plans"), checklist("Plans.", [words("Nothing assigned")]));
    const bob = await pageAs("bob");
    await openUntil(bob, `/s/${space.key}/p/${plans.id}/page#task-0195f000-0000-7000-8000-00000000ffff`, async () => {
      await expect(bob.locator("[data-task-gone]")).toContainText("no longer on this page", ONE_LOOK);
    });
  });

  test("the list of tasks fits a narrow screen and ticks off by touch", { tag: ["@mobile"] }, async ({ api, apiAs, pageAs }, testInfo) => {
    const space = await freshSpace(api, testInfo, "Narrow");
    const bobId = await bobsId(apiAs);
    const long = "Write up the decisions of the long meeting about the quarterly roadmap and send them round";
    await createPage(
      api,
      space.homePageId,
      uniqueName(testInfo, "Roadmap"),
      checklist("Roadmap.", [words(`${long} `), mention(bobId, BOB), words(" "), day(LONG_AGO)]),
    );
    const text = `${long} @${BOB} ${LONG_AGO}`;

    const bob = await pageAs("bob");
    const row = await listed(bob, text);
    await expect(row.locator("[data-task-due]")).toHaveAttribute("data-task-due", "overdue");
    expect(await scrollsSideways(bob)).toBe(false);
    await row.getByRole("checkbox", { name: `Done: ${text}` }).tap();
    await expect(taskRow(bob, text)).toHaveCount(0);
  });

  for (const scheme of ["light", "dark"] as ColourScheme[]) {
    test(`a page's tasks and the list of tasks pass axe in ${scheme}`, async ({ api, apiAs, pageAs }, testInfo) => {
      const space = await freshSpace(api, testInfo, `Axe ${scheme}`);
      const bobId = await bobsId(apiAs);
      const due = await createPage(
        api,
        space.homePageId,
        uniqueName(testInfo, "Due"),
        checklist(
          "Due things.",
          [words("Long overdue "), mention(bobId, BOB), words(" "), day(LONG_AGO)],
          [words("Due now "), mention(bobId, BOB), words(" "), day(TODAY)],
          [words("Whenever "), mention(bobId, BOB)],
        ),
      );
      const bob = await pageAs("bob");
      await startInScheme(bob, scheme);
      await listed(bob, `Whenever @${BOB}`);
      await expect(bob.locator('[data-task-due="overdue"]')).toBeVisible();
      await expect(bob.locator('[data-task-due="today"]')).toBeVisible();
      await expectAccessible(bob);

      await openPage(bob, space.key, due);
      await expect(bob.locator("[data-doc] [data-task-due]")).toHaveCount(2);
      await expectAccessible(bob);
    });
  }
});
