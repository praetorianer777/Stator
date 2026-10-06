import type { Page, TestInfo } from "@playwright/test";
import { must, type StatorApi } from "../fixtures/api";
import { expect } from "../fixtures/auth";
import { caretTo } from "../fixtures/editor";
import { orgTest as test } from "../fixtures/org";
import { openPage } from "../fixtures/replica";
import { uniqueName } from "../fixtures/seed";
import { expectAccessible, startInScheme, type ColourScheme } from "../fixtures/shell";
import { createPage, createSpace, deleteSpace, publishFromEditor, uniqueKey } from "../fixtures/spaces";
import { armatureURL } from "../fixtures/stack";

// The stub makes a person of any name on first use, in the Armature
// organization its token names. alice acts as Armature's admin here, who sees
// SEC; bob acts as himself, who does not.
const patFor = (tenant: string, person: string) => `armature_pat_${tenant}_${person}`;
const SCHEMES: ColourScheme[] = ["light", "dark"];
const BOTH = "project in (CP, SEC) ORDER BY key";
const BAD = 'project = CP AND summary ~ "keys"';

const heading = (page: Page) => page.locator("main").getByRole("heading", { level: 1 });
const list = (page: Page) => page.locator("[data-doc] [data-armature-issue-list]");
const rows = (page: Page) => list(page).locator("[data-issue-row]");

type Body = Parameters<typeof createPage>[3];
const listed = (query: string, columns = ["key", "summary", "status"], limit = 20): Body =>
  ({
    type: "doc",
    content: [
      { type: "paragraph", content: [{ type: "text", text: "Open work." }] },
      { type: "armatureIssueList", attrs: { query, columns, limit } },
    ],
  }) as Body;

async function connect(api: StatorApi, token: string) {
  must(await api.PUT("/armature/account/token", { body: { token } }));
}

test.describe("the Armature issue list", { tag: ["@auth"] }, () => {
  const made: string[] = [];

  test.beforeEach(async ({ api, freshOrg }) => {
    await api.DELETE("/armature/connection");
    must(await api.PUT("/armature/connection", { body: { baseUrl: armatureURL(), orgSlug: freshOrg.slug } }));
    await connect(api, patFor(freshOrg.slug, "admin"));
  });

  test.afterEach(async ({ api }) => {
    for (const key of made.splice(0)) await deleteSpace(api, key);
    await api.DELETE("/armature/connection");
  });

  async function freshSpace(api: StatorApi, testInfo: TestInfo) {
    const key = uniqueKey(testInfo);
    made.push(key);
    return createSpace(api, key, uniqueName(testInfo, "Lists"));
  }

  test("alice inserts a list from the slash menu, and readers sort its rows", async ({ page, api }, testInfo) => {
    const space = await freshSpace(api, testInfo);
    const target = await createPage(api, space.homePageId, uniqueName(testInfo, "Status"), { type: "doc", content: [{ type: "paragraph" }] } as Body);

    await openPage(page, space.key, target);
    await page.locator('[data-action="edit-page"]').click();
    const box = page.locator("#page-body");
    await caretTo(box, "end");
    await page.keyboard.type("/nql");
    await expect(page.locator('[data-slash-item="armatureIssueList"]')).toBeVisible();
    await page.keyboard.press("Enter");

    const dialog = page.getByRole("dialog", { name: "Insert an Armature issue list" });
    const query = dialog.getByRole("textbox", { name: "NQL query" });
    await expect(query).toBeFocused();
    await query.fill(BAD);
    await expect(dialog).toContainText("at character 26");
    await expect(dialog.locator("[data-query-position='26']")).toHaveText("~");
    await query.fill("project = CP ORDER BY key");
    await expect(dialog).toContainText("The query matches 5 issues you can see.");
    await dialog.getByRole("checkbox", { name: "Assignee" }).uncheck();
    await dialog.getByRole("checkbox", { name: "Due" }).check();
    await dialog.getByRole("spinbutton", { name: "Most rows" }).fill("4");
    await expectAccessible(page);
    await dialog.getByRole("button", { name: "Insert" }).click();
    await expect(dialog).toBeHidden();

    const inEditor = box.locator("[data-armature-issue-list]");
    await expect(inEditor.locator("[data-issue-row]")).toHaveCount(4);
    await expectAccessible(page);
    await publishFromEditor(page);

    await expect(rows(page)).toHaveCount(4);
    await expect(list(page).getByRole("columnheader")).toHaveText(["Key", "Summary", "Status", "Due"]);
    await expect(list(page).locator("[data-list-count]")).toHaveText("Showing 4 of 5");
    await expect(rows(page).filter({ hasText: "CP-4" })).toContainText(/Oct 15, 2026/);
    await expect(list(page).getByRole("link", { name: "Open in Armature" })).toHaveAttribute("href", /\/search\?q=project/);
    await expect(rows(page).first().getByRole("link", { name: "CP-1" })).toHaveAttribute("href", `${armatureURL()}/issues/CP-1`);

    const byKey = list(page).getByRole("button", { name: "Key" });
    await byKey.focus();
    await page.keyboard.press("Enter");
    await page.keyboard.press("Enter");
    await expect(list(page).getByRole("columnheader", { name: "Key" })).toHaveAttribute("aria-sort", "descending");
    await expect(rows(page).first()).toHaveAttribute("data-issue-row", "CP-4");
  });

  for (const scheme of SCHEMES) {
    test(`an admin-only issue is not among bob's rows, and a bad query says where, in ${scheme}`, async ({ page, api, apiAs, pageAs, freshOrg }, testInfo) => {
      await startInScheme(page, scheme);
      const space = await freshSpace(api, testInfo);
      const both = await createPage(api, space.homePageId, uniqueName(testInfo, "Everything"), listed(BOTH));
      const broken = await createPage(api, space.homePageId, uniqueName(testInfo, "Broken"), listed(BAD));

      await openPage(page, space.key, both);
      await expect(rows(page)).toHaveCount(6);
      await expect(rows(page).last()).toContainText("Patch the disclosed vulnerability");
      await expectAccessible(page);

      const bobApi = await apiAs("bob");
      await connect(bobApi, patFor(freshOrg.slug, "bob"));
      const bob = await pageAs("bob");
      await startInScheme(bob, scheme);
      await openPage(bob, space.key, both);
      await expect(rows(bob)).toHaveCount(5);
      await expect(list(bob).locator("[data-list-count]")).toHaveText("Showing 5 of 5");
      await expect(bob.locator("main")).not.toContainText("vulnerability");
      await expectAccessible(bob);

      // alice may edit the page: she sees where the query went wrong and can fix it.
      await openPage(page, space.key, broken);
      await expect(list(page)).toHaveAttribute("data-state", "bad_query");
      await expect(list(page)).toContainText('Armature cannot read this query at character 26: "~" cannot be used here.');
      await expect(list(page).locator("[data-query-position='26']")).toHaveText("~");
      await expectAccessible(page);
      await list(page).getByRole("button", { name: "Edit list" }).click();
      await expect(page).toHaveURL(/\/edit$/);

      await bobApi.DELETE("/armature/account/token");
      await bob.reload();
      await expect(heading(bob)).toHaveText(both.title);
      await expect(list(bob)).toHaveAttribute("data-state", "connect");
      await expect(list(bob).getByRole("link", { name: "Connect your account" })).toBeVisible();
      await expect(rows(bob)).toHaveCount(0);
      await expectAccessible(bob);
    });
  }
});
