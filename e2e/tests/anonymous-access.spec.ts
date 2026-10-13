import { must } from "../fixtures/api";
import { expect } from "../fixtures/auth";
import { teamTest as test } from "../fixtures/team";
import { ONE_LOOK, openShowing, openUntil } from "../fixtures/replica";
import { expectAccessible } from "../fixtures/shell";
import { createPage, createSpace, deleteSpace, uniqueKey } from "../fixtures/spaces";
import { WEB_URL } from "../fixtures/stack";

const ANONYMOUS = { cookies: [], origins: [] };
// The spec's own bob, named after it (fixtures/team.ts).
let BOB = "";

test.describe("anonymous access", { tag: ["@auth"] }, () => {
  test.beforeEach(({ team }) => {
    BOB = team.people.bob.name;
  });
  test("alice opens a space to anybody, who reads it with nobody named and is sent to sign in for the rest", async ({ page, api, browser, team }, testInfo) => {
    test.slow();
    const key = uniqueKey(testInfo);
    const word = `openword${Date.now().toString(36)}`;
    const space = await createSpace(api, key, "Open handbook");
    const alice = must(await api.GET("/auth/me")).user;
    const bob = must(await api.GET("/people", { params: { query: { q: BOB } } })).people.find((person) => person.name === BOB);
    expect(bob, "bob is a member to mention").toBeTruthy();
    const secret = await createPage(api, space.homePageId, `Secret ${word}`);
    must(
      await api.PUT("/pages/{pageID}/restrictions", {
        params: { path: { pageID: secret.id } },
        body: { view: [{ type: "user", id: alice.id }], edit: [] },
      }),
    );
    const guide = await createPage(api, space.homePageId, `Guide ${word}`, {
      type: "doc",
      content: [
        {
          type: "paragraph",
          content: [
            { type: "text", text: `Cross at the ${word} with ` },
            { type: "mention", attrs: { id: bob!.id, label: BOB } },
            { type: "text", text: ", or read " },
            { type: "text", text: "the secret", marks: [{ type: "link", attrs: { href: `/s/${key}/p/${secret.id}/secret` } }] },
          ],
        },
      ],
    });
    const context = await browser.newContext({ baseURL: WEB_URL, storageState: ANONYMOUS });
    try {
      await page.goto("/settings/permissions");
      const org = page.locator("[data-anonymous-access]");
      await org.locator('[data-action="anonymous-access"]').click();
      await expect(org.getByRole("status")).toHaveText("Saved.");
      await expect(org.locator("[data-public-address]")).toContainText(`${WEB_URL}/public`);

      await page.goto(`/s/${key}/settings?tab=permissions`);
      const open = page.locator("[data-space-anonymous-access]");
      await open.locator('[data-action="space-anonymous-access"]').click();
      await expect(open.locator("[data-space-public-address]")).toContainText(`/public/s/${key}`);
      await expectAccessible(page);

      // The reader is not alice, so a replica may not have her switches yet.
      const reader = await context.newPage();
      const tree = reader.locator("[data-public-tree]");
      // An address shared while the public pages named the organization still leads there.
      await openShowing(reader, `/public/${team.org.slug}/s/${key}`, tree.getByRole("link", { name: guide.title }));
      await expect(reader).toHaveURL(`${WEB_URL}/public/s/${key}`);
      await expect(reader.locator('meta[name="robots"]')).toHaveAttribute("content", "noindex, nofollow");
      await expect(tree.getByRole("link", { name: secret.title })).toHaveCount(0);
      await expect(reader.locator("[data-rail], [data-sidebar]")).toHaveCount(0);
      await expectAccessible(reader);

      await tree.getByRole("link", { name: guide.title }).click();
      await expect(reader.getByRole("heading", { level: 1 })).toHaveText(guide.title);
      await expect(reader.locator("main [data-mention]")).toHaveText("@someone");
      await expect(reader.locator("main")).not.toContainText(BOB);
      await expect(reader.locator("[data-comments], [data-page-views], [data-reactions]")).toHaveCount(0);

      await reader.getByRole("link", { name: "the secret" }).click();
      await expect(reader.locator("[data-not-public]")).toBeVisible();
      const signIn = reader.locator("[data-not-public]").getByRole("link", { name: "Sign in to read it" });
      await expect(signIn).toHaveAttribute("href", new RegExp(`^/login\\?next=[^&]*$`));

      const hits = reader.locator("[data-public-hit]");
      await openUntil(reader, `/public/s/${key}`, async () => {
        await reader.getByRole("searchbox", { name: "Search the public pages" }).fill(word);
        await reader.getByRole("button", { name: "Search" }).click();
        await expect(hits).toHaveCount(1, ONE_LOOK);
      });
      await expect(hits.first()).toHaveAttribute("data-public-hit", guide.title);
      await expect(reader.locator("[data-public-results]")).not.toContainText(BOB);

      await reader.locator('[data-action="public-sign-in"]').click();
      await expect(reader).toHaveURL(/\/login\?/);
    } finally {
      await context.close();
      must(await api.PUT("/site/anonymous-access", { body: { enabled: false, indexable: false } }));
      await deleteSpace(api, key);
    }
  });
});
