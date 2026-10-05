import type { APIRequestContext, Page, TestInfo } from "@playwright/test";
import { must, type StatorApi } from "../fixtures/api";
import { expect } from "../fixtures/auth";
import { caretTo } from "../fixtures/editor";
import { orgTest as test } from "../fixtures/org";
import { uniqueName } from "../fixtures/seed";
import { expectAccessible, startInScheme, type ColourScheme } from "../fixtures/shell";
import { createPage, createSpace, deleteSpace, publishFromEditor, uniqueKey, type Space } from "../fixtures/spaces";
import { armatureURL } from "../fixtures/stack";

// The stub makes a person of any name on first use, in the Armature
// organization its token names; alice sees CP there.
const patFor = (tenant: string, person: string) => `armature_pat_${tenant}_${person}`;

const editorBox = (page: Page) => page.locator("#page-body");
const shown = (page: Page) => page.locator("main [data-doc]");
const paragraph = (value: string) => ({ type: "paragraph", content: [{ type: "text", text: value }] });

const pad = (n: number) => String(n).padStart(2, "0");
// The browser runs where the suite does, so its days are the suite's.
const now = new Date();
const TODAY = `${now.getFullYear()}-${pad(now.getMonth() + 1)}-${pad(now.getDate())}`;
// Today's day in UTC, which is how an event that lasts all day is kept.
const TODAY_UTC = now.toISOString().slice(0, 10);

/** Opens a page until it holds the words given, which a replica may lag behind on. */
async function open(page: Page, path: string, words: string): Promise<void> {
  await expect(async () => {
    await page.goto(path);
    await expect(page.locator("main")).toContainText(words, { timeout: 2_000 });
  }).toPass();
}

/** Makes CP-1 due today in the stub, so this month's calendar shows it. */
async function dueToday(request: APIRequestContext, tenant: string): Promise<void> {
  const changed = await request.fetch(`${armatureURL()}/_stub/${tenant}/issues/CP-1`, { method: "PATCH", data: { dueDate: TODAY } });
  expect(changed.status()).toBe(200);
}

test.describe("team calendars", { tag: ["@auth"] }, () => {
  const made: string[] = [];

  test.beforeEach(async ({ api, request, freshOrg }) => {
    await dueToday(request, freshOrg.slug);
    await api.DELETE("/armature/connection");
    must(await api.PUT("/armature/connection", { body: { baseUrl: armatureURL(), orgSlug: freshOrg.slug } }));
    must(await api.PUT("/armature/account/token", { body: { token: patFor(freshOrg.slug, "alice") } }));
  });

  test.afterEach(async ({ api }) => {
    for (const key of made.splice(0)) await deleteSpace(api, key);
    await api.DELETE("/armature/connection");
  });

  async function freshSpace(api: StatorApi, testInfo: TestInfo, label: string): Promise<Space> {
    const key = uniqueKey(testInfo);
    made.push(key);
    return createSpace(api, key, uniqueName(testInfo, label));
  }

  test("a lead puts the team's calendar on a page with Armature's due issues, and keeps its events from the page", async ({ page, api, pageAs }, testInfo) => {
    const space = await freshSpace(api, testInfo, "Calendars");
    const notes = await createPage(api, space.homePageId, "Team", { type: "doc", content: [paragraph("Who is where."), { type: "paragraph" }] });
    const path = `/s/${space.key}/p/${notes.id}/team`;

    await open(page, `${path}/edit`, "Who is where.");
    await caretTo(editorBox(page), "end");
    await page.keyboard.type("/calendar");
    await expect(page.getByRole("listbox", { name: "Insert a block" })).toBeVisible();
    await page.keyboard.press("Enter");
    const dialog = page.getByRole("dialog", { name: "Insert a calendar" });
    await expect(dialog.getByLabel("Space")).toHaveValue(space.key);
    // A space without calendars names its first one here.
    await expect(dialog.getByLabel("Calendar", { exact: true })).toHaveValue("new");
    await dialog.getByLabel("Name of the new calendar").fill("Team");
    await expect(dialog.getByLabel("Armature issues").locator("option", { hasText: "(CP)" })).toHaveCount(1);
    await dialog.getByLabel("Armature issues").selectOption("CP");
    await dialog.getByRole("button", { name: "Insert" }).click();
    const inEditor = editorBox(page).getByRole("figure", { name: "Team" });
    await expect(inEditor.locator(`[data-day="${TODAY}"] [data-calendar-issue="CP-1"]`)).toBeVisible();
    await expect(page.locator("[data-draft-status]")).toHaveAttribute("data-draft-status", "saved");
    await publishFromEditor(page);

    const calendar = shown(page).getByRole("figure", { name: "Team" });
    await expect(calendar).toHaveAttribute("data-state", "month");
    const today = calendar.locator(`[data-day="${TODAY}"]`);
    await expect(today.getByRole("link", { name: /CP-1/ })).toHaveAttribute("href", /\/issues\/CP-1$/);

    // Alice may add pages to the space, so she keeps its calendar from the page.
    await calendar.getByRole("button", { name: "Add event" }).click();
    let event = page.getByRole("dialog", { name: "Add an event" });
    await event.getByLabel("Title").fill("Ann on holiday");
    await event.getByLabel("Kind").selectOption("absence");
    await expect(event.getByLabel("First day")).toHaveValue(TODAY);
    await event.getByRole("button", { name: "Add" }).click();
    await expect(event).toBeHidden();
    await expect(today.locator('[data-calendar-event="Ann on holiday"]')).toContainText("Away: Ann on holiday");

    await today.getByRole("button", { name: "Edit Ann on holiday" }).click();
    event = page.getByRole("dialog", { name: "Edit the event" });
    await event.getByLabel("Title").fill("Ann on leave");
    await event.getByRole("button", { name: "Save" }).click();
    await expect(today.locator("[data-calendar-event]")).toHaveText(["Away: Ann on leave"]);
    const kept = must(await api.GET("/spaces/{spaceKey}/calendars", { params: { path: { spaceKey: space.key } } })).calendars;
    expect(kept.map((c) => c.name)).toEqual(["Team"]);
    const events = must(
      await api.GET("/calendars/{calendarID}/events", {
        params: {
          path: { calendarID: kept[0]!.id },
          query: { from: `${TODAY_UTC}T00:00:00Z`, to: new Date(Date.parse(`${TODAY_UTC}T00:00:00Z`) + 86_400_000).toISOString() },
        },
      }),
    ).events;
    expect(events.map((e) => [e.title, e.kind, e.allDay])).toEqual([["Ann on leave", "absence", true]]);

    // Bob has no Armature token: he reads the events, and is asked to connect for the issues.
    const bob = await pageAs("bob");
    // Bob's reads are not held to the publish, so wait for words only it has.
    await open(bob, path, "Connect your Armature account to see the issues due this month.");
    const bobs = bob.locator("main [data-doc]").getByRole("figure", { name: "Team" });
    await expect(bobs.locator(`[data-day="${TODAY}"]`)).toContainText("Ann on leave");
    await expect(bobs).toContainText("Connect your Armature account to see the issues due this month.");

    await today.getByRole("button", { name: "Edit Ann on leave" }).click();
    await page.getByRole("dialog", { name: "Edit the event" }).getByRole("button", { name: "Delete event" }).click();
    await expect(today.locator("[data-calendar-event]")).toHaveCount(0);
  });

  for (const scheme of ["light", "dark"] as ColourScheme[]) {
    test(`a calendar passes axe in ${scheme}`, async ({ page, api }, testInfo) => {
      const space = await freshSpace(api, testInfo, "Axe");
      const team = must(await api.POST("/spaces/{spaceKey}/calendars", { params: { path: { spaceKey: space.key } }, body: { name: "Team" } })).calendar;
      for (const [title, kind] of [
        ["Release", "event"],
        ["Ann away", "absence"],
      ] as const) {
        must(
          await api.POST("/calendars/{calendarID}/events", {
            params: { path: { calendarID: team.id } },
            body: { title, kind, allDay: true, start: `${TODAY_UTC}T00:00:00Z`, end: `${TODAY_UTC}T00:00:00Z` },
          }),
        );
      }
      const notes = await createPage(api, space.homePageId, "Calendar", {
        type: "doc",
        content: [paragraph("This month."), { type: "calendar", attrs: { calendarId: team.id, project: "CP" } }],
      });
      await startInScheme(page, scheme);
      await open(page, `/s/${space.key}/p/${notes.id}/calendar`, "This month.");
      const calendar = shown(page).getByRole("figure", { name: "Team" });
      await expect(calendar).toHaveAttribute("data-state", "month");
      await expect(calendar.locator('[data-calendar-issue="CP-1"]')).toBeVisible();
      await expect(calendar.locator("[data-calendar-event]")).toHaveCount(2);
      await page.screenshot({ path: testInfo.outputPath(`calendar-${scheme}.png`), fullPage: true });
      await expectAccessible(page);
    });
  }
});
