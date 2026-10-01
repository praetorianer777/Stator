import type { Locator, Page, TestInfo } from "@playwright/test";
import { must, type StatorApi } from "../fixtures/api";
import { expect } from "../fixtures/auth";
import { caretTo } from "../fixtures/editor";
import { orgTest as test } from "../fixtures/org";
import { uniqueName } from "../fixtures/seed";
import { expectAccessible, startInScheme, type ColourScheme } from "../fixtures/shell";
import { createPage, createSpace, deleteSpace, publishFromEditor, uniqueKey, type Page as WikiPage } from "../fixtures/spaces";
import { armatureURL } from "../fixtures/stack";

// The stub makes a person of any name on first use, in the Armature
// organization its token names; each spec's organization is its own tenant.
// alice sees CP there, and Armature's admin sees SEC as well.
const patFor = (tenant: string, person: string) => `armature_pat_${tenant}_${person}`;
const SCHEMES: ColourScheme[] = ["light", "dark"];

const heading = (page: Page) => page.locator("main").getByRole("heading", { level: 1 });
const chip = (scope: Page | Locator, key: string) => scope.locator(`[data-armature-issue="${key}"]`);

type Body = Parameters<typeof createPage>[3];
const paragraph = (...content: object[]): Body => ({ type: "doc", content: [{ type: "paragraph", content }] }) as Body;
const issueNode = (key: string) => ({ type: "armatureIssue", attrs: { key } });
const text = (value: string) => ({ type: "text", text: value });

async function openPage(page: Page, spaceKey: string, target: WikiPage) {
  await expect(async () => {
    await page.goto(`/s/${spaceKey}/p/${target.id}/page`);
    await expect(heading(page)).toHaveText(target.title, { timeout: 1_000 });
  }).toPass();
}

/** Pastes text into the focused editor the way a browser does, as plain text alone. */
async function paste(box: Locator, value: string) {
  await box.evaluate((el, data) => {
    const transfer = new DataTransfer();
    transfer.setData("text/plain", data);
    el.dispatchEvent(new ClipboardEvent("paste", { clipboardData: transfer, bubbles: true, cancelable: true }));
  }, value);
}

async function connect(api: StatorApi, token: string) {
  must(await api.PUT("/armature/account/token", { body: { token } }));
}

test.describe("smart links to Armature issues", { tag: ["@auth"] }, () => {
  const made: string[] = [];

  test.beforeEach(async ({ api, freshOrg }) => {
    await api.DELETE("/armature/connection");
    must(await api.PUT("/armature/connection", { body: { baseUrl: armatureURL(), orgSlug: freshOrg.slug } }));
    await connect(api, patFor(freshOrg.slug, "alice"));
  });

  test.afterEach(async ({ api }) => {
    for (const key of made.splice(0)) await deleteSpace(api, key);
    await api.DELETE("/armature/connection");
  });

  async function freshSpace(api: StatorApi, testInfo: TestInfo) {
    const key = uniqueKey(testInfo);
    made.push(key);
    return createSpace(api, key, uniqueName(testInfo, "Links"));
  }

  test("alice types a key and pastes an issue address, and both become chips that stay after publishing", async ({ page, api }, testInfo) => {
    const space = await freshSpace(api, testInfo);
    const notes = await createPage(api, space.homePageId, uniqueName(testInfo, "Release notes"), paragraph(text("Notes.")));

    await openPage(page, space.key, notes);
    // A key typed before the author's projects are known stays text.
    const projects = page.waitForResponse((res) => new URL(res.url()).pathname === "/api/v1/armature/projects" && res.ok());
    await page.locator('[data-action="edit-page"]').click();
    await projects;
    const box = page.locator("#page-body");
    await caretTo(box, "end");
    await page.keyboard.type(" Fixed in CP-1, unlike UTF-8 and SEC-1. See ");
    const typed = chip(box, "CP-1");
    await expect(typed).toContainText("Set up the build pipeline");
    await expect(chip(box, "SEC-1")).toHaveCount(0);
    await expect(box).toContainText("UTF-8 and SEC-1.");

    await paste(box, `${armatureURL()}/issues/sec-2/`);
    const pasted = chip(box, "SEC-2");
    await expect(pasted).toContainText("CP-5");
    await expect(pasted).toContainText("Audit the token scopes");
    await expectAccessible(page);

    await publishFromEditor(page);
    const view = page.locator("[data-doc]");
    const link = chip(view, "CP-1");
    await expect(link).toHaveAttribute("href", `${armatureURL()}/issues/CP-1`);
    await expect(link.locator("[data-status-category]")).toHaveText(/done/i);
    await expect(chip(view, "SEC-2")).toHaveAttribute("href", `${armatureURL()}/issues/CP-5`);

    await link.focus();
    const card = page.locator("[data-armature-card]");
    await expect(card).toContainText("Set up the build pipeline");
    await expect(card).toContainText("Alice");
    await expectAccessible(page);
  });

  for (const scheme of SCHEMES) {
    test(`an issue only admins see, and a viewer without a token, in ${scheme}`, async ({ page, api, apiAs, pageAs, freshOrg }, testInfo) => {
      await startInScheme(page, scheme);
      const space = await freshSpace(api, testInfo);
      const target = await createPage(
        api,
        space.homePageId,
        uniqueName(testInfo, "Security review"),
        paragraph(text("Tracked in "), issueNode("SEC-1"), text(" and "), issueNode("CP-2"), text(".")),
      );

      // alice is not Armature's admin: the secret issue is not hers to see.
      await openPage(page, space.key, target);
      const hidden = chip(page.locator("[data-doc]"), "SEC-1");
      await expect(hidden).toHaveAttribute("data-state", "hidden");
      await expect(hidden).toContainText("Not available");
      await expect(page.locator("main")).not.toContainText("vulnerability");
      await expect(chip(page.locator("[data-doc]"), "CP-2")).toContainText("Sign-in fails with an expired session");
      await expectAccessible(page);

      // bob acts as Armature's admin, who sees it.
      const bobApi = await apiAs("bob");
      await connect(bobApi, patFor(freshOrg.slug, "admin"));
      const bob = await pageAs("bob");
      await startInScheme(bob, scheme);
      await openPage(bob, space.key, target);
      await expect(chip(bob.locator("[data-doc]"), "SEC-1")).toContainText("Patch the disclosed vulnerability");
      await expectAccessible(bob);

      // Without a token bob sees the keys and the way to connect, and nothing else.
      await bobApi.DELETE("/armature/account/token");
      await bob.reload();
      await expect(heading(bob)).toHaveText(target.title);
      const bare = chip(bob.locator("[data-doc]"), "SEC-1");
      await expect(bare).toHaveAttribute("data-state", "connect");
      await expect(bare.getByRole("link", { name: "SEC-1" })).toHaveAttribute("href", `${armatureURL()}/issues/SEC-1`);
      await expect(bob.locator("main")).not.toContainText("vulnerability");
      await expect(bob.locator("main")).not.toContainText("Sign-in fails");
      await expectAccessible(bob);
      await bare.getByRole("link", { name: "Connect Armature to see this issue" }).click();
      await expect(bob.locator("[data-armature-account]")).toBeVisible();
    });
  }
});
