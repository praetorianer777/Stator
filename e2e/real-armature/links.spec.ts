import { expect } from "../fixtures/auth";
import { openPage } from "../fixtures/replica";
import { uniqueName } from "../fixtures/seed";
import { createPage, createSpace, deleteSpace, uniqueKey } from "../fixtures/spaces";
import { WEB_URL } from "../fixtures/stack";
import { teamTest as test } from "../fixtures/team";
import { answered, armatureAs, connectAccount, connectSite, readSeed } from "./armature";

// A published page that names an issue is listed on it in the real Armature,
// through the stack's worker; trashing the page takes the link away again.

type Body = Parameters<typeof createPage>[3];
type RemoteLink = { url: string; title: string; source: string; relation: string; createdBy: string };

test.describe("pages linked in a real Armature", { tag: ["@auth"] }, () => {
  let spaceKey = "";

  test.beforeEach(async ({ api }) => {
    const seed = readSeed();
    await connectSite(api, seed);
    await connectAccount(api, seed.people.member);
  });

  test.afterEach(async ({ api }) => {
    if (spaceKey) await deleteSpace(api, spaceKey);
    await api.DELETE("/armature/connection");
  });

  test("publishing a page puts its link on the issue, and trashing the page takes it off", async ({ page, api }, testInfo) => {
    const seed = readSeed();
    const { runbooks } = seed.issues;
    spaceKey = uniqueKey(testInfo);
    const space = await createSpace(api, spaceKey, uniqueName(testInfo, "Runbooks"));
    const target = await createPage(api, space.homePageId, uniqueName(testInfo, "Runbook index"), {
      type: "doc",
      content: [
        {
          type: "paragraph",
          content: [
            { type: "text", text: "Tracked in " },
            { type: "armatureIssue", attrs: { key: runbooks.key } },
          ],
        },
      ],
    } as Body);
    const url = `${WEB_URL}/spaces/${space.key}/p/${target.id}`;

    await openPage(page, space.key, target);
    const row = page.locator(`[data-armature-links] [data-armature-link="${runbooks.key}"]`);
    await expect(row).toHaveAttribute("data-state", "synced");
    await expect(row.getByRole("link", { name: runbooks.key })).toHaveAttribute("href", `${seed.url}/issues/${runbooks.key}`);

    const ada = await armatureAs(seed.people.admin.token);
    try {
      const linksOnIssue = async () =>
        (await answered<{ remoteLinks: RemoteLink[] }>(ada.get(`issues/${runbooks.key}/remote-links`), 200, "the issue's links")).remoteLinks.filter(
          (each) => each.url === url,
        );
      await expect.poll(linksOnIssue).toHaveLength(1);
      const [link] = await linksOnIssue();
      expect(link).toMatchObject({ title: target.title, source: "Stator", relation: "page", createdBy: seed.people.member.id });

      expect((await api.DELETE("/pages/{pageID}", { params: { path: { pageID: target.id } } })).response.status).toBe(204);
      await expect.poll(linksOnIssue).toHaveLength(0);
    } finally {
      await ada.dispose();
    }
  });
});
