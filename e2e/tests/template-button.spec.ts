import type { Page, TestInfo } from "@playwright/test";
import { must, type StatorApi } from "../fixtures/api";
import { expect } from "../fixtures/auth";
import { caretTo } from "../fixtures/editor";
import { orgTest as test } from "../fixtures/org";
import { uniqueName } from "../fixtures/seed";
import { expectAccessible } from "../fixtures/shell";
import { childTitles, createPage, createSpace, deleteSpace, publishFromEditor, uniqueKey, type Space } from "../fixtures/spaces";

const editorBox = (page: Page) => page.locator("#page-body");
const shown = (page: Page) => page.locator("main [data-doc]");

/** The reader's own day, as the button names a page with it. */
function today(): string {
  const now = new Date();
  const pad = (n: number) => String(n).padStart(2, "0");
  return `${now.getFullYear()}-${pad(now.getMonth() + 1)}-${pad(now.getDate())}`;
}

/** Opens a page until it holds the words given, which a replica may lag behind on. */
async function open(page: Page, path: string, text: string): Promise<void> {
  await expect(async () => {
    await page.goto(path);
    await expect(page.locator("main")).toContainText(text, { timeout: 2_000 });
  }).toPass();
}

async function insertBlock(page: Page, typed: string): Promise<void> {
  await caretTo(editorBox(page), "end");
  await page.keyboard.press("Enter");
  await page.keyboard.type(typed);
  await expect(page.getByRole("listbox", { name: "Insert a block" })).toBeVisible();
  await page.keyboard.press("Enter");
}

test.describe("template button and contributors", { tag: ["@auth"] }, () => {
  const made: string[] = [];
  test.afterEach(async ({ api }) => {
    for (const key of made.splice(0)) await deleteSpace(api, key);
  });

  async function freshSpace(api: StatorApi, testInfo: TestInfo): Promise<Space> {
    const key = uniqueKey(testInfo);
    made.push(key);
    const space = await createSpace(api, key, uniqueName(testInfo, "Meetings"));
    // Everybody reads the space; only alice, who made it, adds pages.
    must(
      await api.PUT("/spaces/{spaceKey}/permissions", {
        params: { path: { spaceKey: key } },
        body: { grants: [{ subject: { type: "everyone" }, permissions: ["view"] }] },
      }),
    );
    return space;
  }

  test("alice puts a button for meeting notes and who wrote the page on it; a click makes the notes, and bob, who only reads, may not", async ({
    page,
    api,
    pageAs,
  }, testInfo) => {
    const space = await freshSpace(api, testInfo);
    const alice = must(await api.GET("/auth/me")).user;
    const meetings = await createPage(api, space.homePageId, "Team meetings", {
      type: "doc",
      content: [{ type: "paragraph", content: [{ type: "text", text: "Our weekly meetings." }] }],
    });
    const path = `/s/${space.key}/p/${meetings.id}/team-meetings`;

    await open(page, `${path}/edit`, "Our weekly meetings.");
    await insertBlock(page, "/template");
    const dialog = page.getByRole("dialog", { name: "Insert a template button" });
    await dialog.getByLabel("Template", { exact: true }).selectOption({ label: "Meeting notes" });
    await dialog.getByLabel("Put new pages").selectOption({ label: "Team meetings" });
    await dialog.getByLabel("Button text").fill("New meeting notes");
    await dialog.getByLabel("Title of each new page").fill("Weekly sync {date}");
    await dialog.getByRole("button", { name: "Insert" }).click();
    const inEditor = editorBox(page).locator("[data-template-button]");
    await expect(inEditor.getByRole("button", { name: "New meeting notes" })).toBeDisabled();
    await expect(inEditor).toContainText("does nothing while you edit");

    await insertBlock(page, "/contributors");
    await expect(editorBox(page).locator("[data-contributor]")).toHaveText([new RegExp(alice.name)]);
    await expect(page.locator("[data-draft-status]")).toHaveAttribute("data-draft-status", "saved");
    await publishFromEditor(page);

    const people = shown(page).getByRole("region", { name: "Contributors to this page" });
    await expect(people.locator("[data-contributor]")).toHaveText([new RegExp(`${alice.name}.*2 versions`)]);
    const button = shown(page).getByRole("button", { name: "New meeting notes" });
    await expect(button).toBeEnabled();
    await expect(button).toHaveAccessibleDescription("Makes a page from Meeting notes under Team meetings and opens it for you to write.");
    await expectAccessible(page);

    const bob = await pageAs("bob");
    await open(bob, path, "Our weekly meetings.");
    const refused = shown(bob).getByRole("button", { name: "New meeting notes" });
    await expect(refused).toBeDisabled();
    await expect(refused).toHaveAccessibleDescription("You may not add pages under Team meetings. Ask an administrator of the space to let you add pages.");
    await expect(shown(bob).locator("[data-contributor]")).toHaveText([new RegExp(alice.name)]);

    await button.click();
    await page.waitForURL(/\/edit$/);
    const title = `Weekly sync ${today()}`;
    await expect(page.getByLabel("Title", { exact: true })).toHaveValue(title);
    await expect(editorBox(page).getByRole("heading", { name: "Action items" })).toBeVisible();
    expect(await childTitles(api, space.key, meetings.id)).toEqual([title]);
  });
});
