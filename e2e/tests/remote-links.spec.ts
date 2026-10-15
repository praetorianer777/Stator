import type { Page, TestInfo } from "@playwright/test";
import { must, type StatorApi } from "../fixtures/api";
import { expect } from "../fixtures/auth";
import { teamTest as test } from "../fixtures/team";
import { openShowing } from "../fixtures/replica";
import { uniqueName } from "../fixtures/seed";
import { expectAccessible, startInScheme, type ColourScheme } from "../fixtures/shell";
import { createPage, createSpace, deleteSpace, uniqueKey, type Space } from "../fixtures/spaces";

const panel = (page: Page) => page.locator("[data-remote-links]");
const row = (page: Page, url: string) => page.locator(`[data-remote-links] [data-remote-link="${url}"]`);

const requirement = (n: number) => ({
  url: `https://exciter.example/r/${n}`,
  title: `REQ-${n} Sign in with a passkey (specifies)`,
  source: "Exciter",
  iconUrl: "https://exciter.example/favicon-32.png",
});
const issue = { url: "https://armature.example/issues/CP-1", title: "CP-1 Passkeys", source: "Armature" };

async function putLink(api: StatorApi, pageId: string, body: { url: string; title: string; source: string; iconUrl?: string }): Promise<string> {
  return must(await api.POST("/pages/{pageID}/remote-links", { params: { path: { pageID: pageId } }, body })).remoteLink.id;
}

test.describe("remote links", { tag: ["@auth"] }, () => {
  const made: string[] = [];
  test.afterEach(async ({ api }) => {
    for (const key of made.splice(0)) await deleteSpace(api, key);
  });

  async function freshSpace(api: StatorApi, testInfo: TestInfo, label: string): Promise<Space> {
    const key = uniqueKey(testInfo);
    made.push(key);
    return createSpace(api, key, uniqueName(testInfo, label));
  }

  test("a page lists the links other tools put on it by source, and an editor takes one off", async ({ page, api }, testInfo) => {
    const space = await freshSpace(api, testInfo, "Remote");
    const target = await createPage(api, space.homePageId, "Passkey specification");
    const empty = await createPage(api, space.homePageId, "Unlinked notes");
    await putLink(api, target.id, requirement(1));
    await putLink(api, target.id, requirement(2));
    await putLink(api, target.id, issue);

    await openShowing(page, `/s/${space.key}/p/${target.id}/passkey-specification`, row(page, requirement(1).url));
    await expect(panel(page).getByRole("heading", { level: 3 })).toHaveText(["Armature (1)", "Exciter (2)"]);
    const first = row(page, requirement(1).url);
    await expect(first.getByRole("link", { name: requirement(1).title })).toHaveAttribute("href", requirement(1).url);
    await expect(first).toContainText("Put by");
    // Nothing answers for exciter.example, and the page's policy keeps other servers' pictures out besides, so the source's initial stands in.
    await expect(first.locator("img")).toHaveCount(0);
    await expect(first).toContainText("E");

    await first.getByRole("button", { name: `Remove the link ${requirement(1).title}` }).click();
    await expect(first).toHaveCount(0);
    await expect(panel(page).getByRole("heading", { level: 3 })).toHaveText(["Armature (1)", "Exciter (1)"]);
    await expect(page.locator("[data-remote-link-notice]")).toContainText(`Removed the link ${requirement(1).title}.`);
    const left = must(await api.GET("/pages/{pageID}/remote-links", { params: { path: { pageID: target.id } } })).remoteLinks;
    expect(left.map((each) => each.url).sort()).toEqual([issue.url, requirement(2).url].sort());

    await page.goto(`/s/${space.key}/p/${empty.id}/unlinked-notes`);
    await expect(page.locator("main").getByRole("heading", { level: 1 })).toHaveText("Unlinked notes");
    await expect(panel(page)).toHaveCount(0);
  });

  test("a member who may not edit reads the links and is offered no way to take one off", async ({ api, apiAs, pageAs }, testInfo) => {
    const space = await freshSpace(api, testInfo, "Read only");
    const target = await createPage(api, space.homePageId, "Charter");
    await putLink(api, target.id, requirement(3));
    must(
      await api.PUT("/spaces/{spaceKey}/permissions", {
        params: { path: { spaceKey: space.key } },
        body: { grants: [{ subject: { type: "everyone" }, permissions: ["view"] }] },
      }),
    );
    const bobApi = await apiAs("bob");
    await expect.poll(async () => (await bobApi.GET("/pages/{pageID}", { params: { path: { pageID: target.id } } })).data?.page.can.edit).toBe(false);
    expect((await bobApi.POST("/pages/{pageID}/remote-links", { params: { path: { pageID: target.id } }, body: requirement(4) })).response.status).toBe(403);

    const bob = await pageAs("bob");
    await openShowing(bob, `/s/${space.key}/p/${target.id}/charter`, row(bob, requirement(3).url));
    await expect(panel(bob).getByRole("button")).toHaveCount(0);
  });

  for (const scheme of ["light", "dark"] as ColourScheme[]) {
    test(`the panel passes axe in ${scheme}`, async ({ page, api }, testInfo) => {
      const space = await freshSpace(api, testInfo, "Axe");
      const target = await createPage(api, space.homePageId, "Puffin requirements");
      await putLink(api, target.id, requirement(5));
      await putLink(api, target.id, issue);
      await startInScheme(page, scheme);
      await openShowing(page, `/s/${space.key}/p/${target.id}/puffin-requirements`, row(page, issue.url));
      await expect(page.locator("html")).toHaveAttribute("data-theme", scheme);
      await expectAccessible(page);
    });
  }
});
