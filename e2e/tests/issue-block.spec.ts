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

const heading = (page: Page) => page.locator("main").getByRole("heading", { level: 1 });
const block = (page: Page, key: string) => page.locator(`[data-doc] [data-armature-issue-block="${key}"]`);

type Body = Parameters<typeof createPage>[3];
const blocks = (...keys: string[]): Body =>
  ({
    type: "doc",
    content: [
      { type: "paragraph", content: [{ type: "text", text: "Tracked work." }] },
      ...keys.map((key) => ({ type: "armatureIssueBlock", attrs: { key } })),
    ],
  }) as Body;

async function connect(api: StatorApi, token: string) {
  must(await api.PUT("/armature/account/token", { body: { token } }));
}

test.describe("the Armature issue block", { tag: ["@auth"] }, () => {
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
    return createSpace(api, key, uniqueName(testInfo, "Blocks"));
  }

  test("alice inserts an issue from the slash menu by its key, and readers see its details", async ({ page, api }, testInfo) => {
    const space = await freshSpace(api, testInfo);
    const target = await createPage(api, space.homePageId, uniqueName(testInfo, "Key rotation"), blocks());

    await openPage(page, space.key, target);
    await page.locator('[data-action="edit-page"]').click();
    const box = page.locator("#page-body");
    await caretTo(box, "end");
    await page.keyboard.press("Enter");
    await page.keyboard.type("/ticket");
    await expect(page.locator('[data-slash-item="armatureIssue"]')).toBeVisible();
    await page.keyboard.press("Enter");

    const dialog = page.getByRole("dialog", { name: "Insert an Armature issue" });
    const field = dialog.getByRole("textbox", { name: "Issue key or address" });
    await expect(field).toBeFocused();
    await field.fill("CP-99");
    await expect(dialog).toContainText("No issue CP-99 that you can see in Armature.");
    await field.fill(`${armatureURL()}/issues/cp-4`);
    await expect(dialog.locator("[data-picked]")).toContainText("Rotate the signing keys");
    await expectAccessible(page);
    await page.keyboard.press("Enter");
    await expect(dialog).toBeHidden();

    const inEditor = box.locator('[data-armature-issue-block="CP-4"]');
    await expect(inEditor).toContainText("Rotate the signing keys");
    await expectAccessible(page);
    await publishFromEditor(page);

    const card = block(page, "CP-4");
    await expect(card).toHaveAttribute("data-state", "issue");
    await expect(card).toContainText("Rotate the signing keys");
    await expect(card.locator('[data-field="assignee"]')).toContainText("Alice");
    await expect(card.locator('[data-field="reporter"]')).toContainText("Bob");
    await expect(card.locator('[data-field="priority"]')).toContainText("Highest");
    await expect(card.locator('[data-field="due"]')).toContainText(/Oct 15, 2026/);
    await expect(card.getByRole("link", { name: "Open in Armature CP-4" })).toHaveAttribute("href", `${armatureURL()}/issues/CP-4`);

    // The block is a link and a card to the keyboard: Tab reaches its link.
    await card.getByRole("link", { name: "Open in Armature CP-4" }).focus();
    await expect(card.getByRole("link", { name: "Open in Armature CP-4" })).toBeFocused();

    // A comparison says which issue the block names, in words.
    await page.goto(`/s/${space.key}/p/${target.id}/page/history?from=1&to=2`);
    await expect(page.locator("[data-diff-view]")).toContainText("Armature issue CP-4");
    await expect(page.locator("[data-diff-view]")).not.toContainText("Rotate the signing keys");
  });

  for (const scheme of SCHEMES) {
    test(`an issue only admins see is hidden from bob, and without a token he sees the key, in ${scheme}`, async ({
      page,
      api,
      apiAs,
      pageAs,
      freshOrg,
    }, testInfo) => {
      await startInScheme(page, scheme);
      const space = await freshSpace(api, testInfo);
      const target = await createPage(api, space.homePageId, uniqueName(testInfo, "Security review"), blocks("SEC-1", "CP-2"));

      await openPage(page, space.key, target);
      await expect(block(page, "SEC-1")).toContainText("Patch the disclosed vulnerability");
      await expect(block(page, "CP-2")).toContainText("Sign-in fails with an expired session");
      await expectAccessible(page);

      const bobApi = await apiAs("bob");
      await connect(bobApi, patFor(freshOrg.slug, "bob"));
      const bob = await pageAs("bob");
      await startInScheme(bob, scheme);
      await openPage(bob, space.key, target);
      await expect(block(bob, "CP-2")).toContainText("Sign-in fails with an expired session");
      await expect(block(bob, "SEC-1")).toHaveAttribute("data-state", "hidden");
      await expect(block(bob, "SEC-1")).toContainText("Not available");
      await expect(bob.locator("main")).not.toContainText("vulnerability");
      await expectAccessible(bob);

      await bobApi.DELETE("/armature/account/token");
      await bob.reload();
      await expect(heading(bob)).toHaveText(target.title);
      await expect(block(bob, "CP-2")).toHaveAttribute("data-state", "connect");
      await expect(block(bob, "CP-2").getByRole("link", { name: "Connect Armature to see this issue" })).toBeVisible();
      await expect(bob.locator("main")).not.toContainText("Sign-in fails");
      await expectAccessible(bob);
    });
  }
});
