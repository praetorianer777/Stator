import { expect } from "../fixtures/auth";
import { focusEditor } from "../fixtures/editor";
import { openPage } from "../fixtures/replica";
import { uniqueName } from "../fixtures/seed";
import { createPage, createSpace, deleteSpace, uniqueKey } from "../fixtures/spaces";
import { teamTest as test } from "../fixtures/team";
import { answered, armatureAs, connectAccount, connectSite, readSeed } from "./armature";

// Filing issues in the real Armature from text selected on a page, as Mia.

type Body = Parameters<typeof createPage>[3];

test.describe("creating issues in a real Armature from a selection", { tag: ["@auth"] }, () => {
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

  test("alice files a selected line in Armature, and its chip takes the line's place", async ({ page, api }, testInfo) => {
    const seed = readSeed();
    const summary = uniqueName(testInfo, "Write the release notes");
    spaceKey = uniqueKey(testInfo);
    const space = await createSpace(api, spaceKey, uniqueName(testInfo, "Reviews"));
    const target = await createPage(api, space.homePageId, uniqueName(testInfo, "Review notes"), {
      type: "doc",
      content: [
        { type: "paragraph", content: [{ type: "text", text: "Action items." }] },
        { type: "paragraph", content: [{ type: "text", text: summary }] },
      ],
    } as Body);

    await openPage(page, space.key, target);
    await page.locator('[data-action="edit-page"]').click();
    const box = page.locator("#page-body");
    await focusEditor(box.locator("p", { hasText: summary }));
    await page.keyboard.press("End");
    await page.keyboard.press("Shift+Home");
    const create = page.locator('[data-editor-tools="selection"] [data-editor-action="create-issues"]');
    await expect(create).toHaveText("Create Armature issue");
    await create.click();

    const dialog = page.locator("[data-create-issues-dialog]");
    await expect(dialog.getByRole("combobox", { name: "Project" }).locator("option")).toHaveText([`${seed.project.key}: ${seed.project.name}`]);
    await expect(dialog.getByRole("textbox", { name: "Summary of issue 1" })).toHaveValue(summary);
    await dialog.getByRole("combobox", { name: "Issue type" }).selectOption({ label: "Task" });
    await dialog.locator('[data-action="create-issues"]').click();
    const created = dialog.locator('[data-result="created"]');
    await expect(created).toHaveCount(1);
    const key = /^([A-Z][A-Z0-9]+-\d+) /.exec((await created.textContent()) ?? "")?.[1];
    expect(key, "the dialog names the issue Armature filed").toMatch(new RegExp(`^${seed.project.key}-\\d+$`));
    await dialog.locator('[data-action="close-created"]').click();
    await expect(box.locator(`[data-armature-issue="${key}"]`)).toBeVisible();

    // Armature has it, filed by Mia, of the type chosen, pointing back at the page.
    const mia = await armatureAs(seed.people.member.token);
    try {
      const { issue } = await answered<{ issue: { summary: string; reporter: { name: string }; type: { name: string }; description?: unknown } }>(
        mia.get(`issues/${key}`),
        200,
        `the issue ${key}`,
      );
      expect(issue.summary).toBe(summary);
      expect(issue.reporter.name).toBe(seed.people.member.name);
      expect(issue.type.name).toBe("Task");
      expect(JSON.stringify(issue.description)).toContain(`/spaces/${space.key}/p/`);
    } finally {
      await mia.dispose();
    }
  });
});
