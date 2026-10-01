import type { APIRequestContext, Locator, Page, TestInfo } from "@playwright/test";
import { must, type StatorApi } from "../fixtures/api";
import { expect } from "../fixtures/auth";
import { focusEditor } from "../fixtures/editor";
import { orgTest as test } from "../fixtures/org";
import { uniqueName } from "../fixtures/seed";
import { expectAccessible, startInScheme, type ColourScheme } from "../fixtures/shell";
import { createPage, createSpace, deleteSpace, publishFromEditor, uniqueKey, type Page as WikiPage } from "../fixtures/spaces";
import { armatureURL, WEB_URL } from "../fixtures/stack";

// The stub makes a person of any name on first use, in the Armature
// organization its token names. alice acts as Armature's admin here, who sees
// SEC; the stub is told she may only read it, so she may file in CP alone.
const patFor = (tenant: string, person: string) => `armature_pat_${tenant}_${person}`;
const SCHEMES: ColourScheme[] = ["light", "dark"];

const heading = (page: Page) => page.locator("main").getByRole("heading", { level: 1 });

type Body = Parameters<typeof createPage>[3];
const text = (value: string) => ({ type: "text", text: value });
const para = (value: string) => ({ type: "paragraph", content: [text(value)] });
const cell = (type: "tableCell" | "tableHeader", value: string) => ({
  type,
  content: [value ? para(value) : { type: "paragraph" }],
});
const actionItems: Body = {
  type: "doc",
  content: [
    para("Action items from the review."),
    para("Renew the TLS certificate"),
    {
      type: "bulletList",
      content: ["Write the migration guide", "Update the status page", "Tell the support team"].map((item) => ({ type: "listItem", content: [para(item)] })),
    },
    {
      type: "table",
      content: [
        {
          type: "tableRow",
          content: [cell("tableHeader", "Task"), cell("tableHeader", "Owner")],
        },
        {
          type: "tableRow",
          content: [cell("tableCell", "Archive the old logs"), cell("tableCell", "Alice")],
        },
        {
          type: "tableRow",
          content: [cell("tableCell", ""), cell("tableCell", "Rotate the backup keys")],
        },
      ],
    },
  ],
} as Body;

async function openPage(page: Page, spaceKey: string, target: WikiPage) {
  await expect(async () => {
    await page.goto(`/s/${spaceKey}/p/${target.id}/page`);
    await expect(heading(page)).toHaveText(target.title, { timeout: 1_000 });
  }).toPass();
}

async function connect(api: StatorApi, token: string) {
  must(await api.PUT("/armature/account/token", { body: { token } }));
}

async function stub(request: APIRequestContext, method: "GET" | "PUT" | "DELETE", path: string, data?: unknown) {
  const answer = await request.fetch(`${armatureURL()}/_stub/${path}`, {
    method,
    data,
  });
  expect(answer.ok(), `the stub answered ${method} ${path} with ${answer.status()}`).toBe(true);
  return answer.status() === 204 ? null : ((await answer.json()) as Record<string, unknown>);
}

/** Selects one paragraph's or list item's text, from the keyboard. */
async function selectLine(box: Locator, words: string) {
  await focusEditor(box.locator("p", { hasText: words }).first());
  await box.page().keyboard.press("End");
  await box.page().keyboard.press("Shift+Home");
}

const chip = (scope: Locator | Page, key: string) => scope.locator(`[data-armature-issue="${key}"]`);
const createButton = (page: Page) => page.locator('[data-editor-tools="selection"] [data-editor-action="create-issues"]');

test.describe("creating Armature issues from a selection", {
  tag: ["@auth"],
}, () => {
  const made: string[] = [];

  test.beforeEach(async ({ api, freshOrg, request }) => {
    await stub(request, "DELETE", freshOrg.slug);
    await stub(request, "PUT", `${freshOrg.slug}/people/admin/read-only-projects`, { projects: ["SEC"] });
    await api.DELETE("/armature/connection");
    must(
      await api.PUT("/armature/connection", {
        body: { baseUrl: armatureURL(), orgSlug: freshOrg.slug },
      }),
    );
    await connect(api, patFor(freshOrg.slug, "admin"));
  });

  test.afterEach(async ({ api }) => {
    for (const key of made.splice(0)) await deleteSpace(api, key);
    await api.DELETE("/armature/connection");
  });

  async function freshSpace(api: StatorApi, testInfo: TestInfo) {
    const key = uniqueKey(testInfo);
    made.push(key);
    return createSpace(api, key, uniqueName(testInfo, "Reviews"));
  }

  // A test per kind of selection rather than one for all three: the
  // whole-page accessibility scans are most of each flow's time, and together
  // they ran up against the test timeout on a busy runner.
  async function openInEditor(page: Page, api: StatorApi, testInfo: TestInfo, scheme: ColourScheme) {
    await startInScheme(page, scheme);
    const space = await freshSpace(api, testInfo);
    const target = await createPage(api, space.homePageId, uniqueName(testInfo, "Review notes"), actionItems);
    await openPage(page, space.key, target);
    await page.locator('[data-action="edit-page"]').click();
    return { space, target, box: page.locator("#page-body"), dialog: page.locator("[data-create-issues-dialog]") };
  }

  for (const scheme of SCHEMES) {
    test(`alice files a sentence as an issue, and its chip takes the text's place, in ${scheme}`, async ({ page, api }, testInfo) => {
      const { box, dialog } = await openInEditor(page, api, testInfo, scheme);

      await selectLine(box, "Renew the TLS certificate");
      await expect(createButton(page)).toHaveText("Create Armature issue");
      await createButton(page).click();
      await expect(dialog.getByRole("heading")).toHaveText("Create an Armature issue");
      const project = dialog.getByRole("combobox", { name: "Project" });
      await expect(project.locator("option")).toHaveText(["CP: Core platform"]);
      await expect(dialog.getByRole("textbox", { name: "Summary of issue 1" })).toHaveValue("Renew the TLS certificate");
      await dialog.getByRole("combobox", { name: "Issue type" }).selectOption({ label: "Bug" });
      await expectAccessible(page);
      await dialog.locator('[data-action="create-issues"]').click();
      await expect(dialog.locator('[data-result="created"]')).toHaveText(["CP-6 Renew the TLS certificate"]);
      await expectAccessible(page);
      await dialog.locator('[data-action="close-created"]').click();
      await expect(dialog).toBeHidden();
      await expect(chip(box, "CP-6")).toBeVisible();
      const own = (p: Element) => [...p.childNodes].filter((n) => n.nodeType === Node.TEXT_NODE && n.textContent?.trim()).length;
      expect(await box.locator("p", { has: chip(page, "CP-6") }).evaluate(own)).toBe(0);

      // One undo takes the chip back out and the text back in.
      await focusEditor(box.locator("p", { hasText: "Action items" }));
      await page.keyboard.press("ControlOrMeta+z");
      await expect(chip(box, "CP-6")).toHaveCount(0);
      await expect(box).toContainText("Renew the TLS certificate");
      await page.keyboard.press("ControlOrMeta+Shift+z");
      await expect(chip(box, "CP-6")).toBeVisible();

      await publishFromEditor(page);
      const doc = page.locator("[data-doc]");
      await expect(chip(doc, "CP-6")).toBeVisible();
      await expect(chip(doc, "CP-6")).toContainText("Renew the TLS certificate");
    });

    test(`alice files list items as issues until Armature refuses one, and each chip follows its item, in ${scheme}`, async ({
      page,
      api,
      freshOrg,
      request,
    }, testInfo) => {
      const { box, dialog } = await openInEditor(page, api, testInfo, scheme);

      // Armature refuses the second, so the third is not tried.
      await stub(request, "PUT", `${freshOrg.slug}/refused-summary`, {
        summary: "Update the status page",
      });
      await focusEditor(box.locator("li p", { hasText: "Write the migration guide" }));
      await page.keyboard.press("Home");
      await page.keyboard.press("Shift+ArrowDown");
      await page.keyboard.press("Shift+ArrowDown");
      await page.keyboard.press("Shift+End");
      await expect(createButton(page)).toHaveText("Create 3 Armature issues");
      await createButton(page).click();
      await expect(dialog.getByRole("textbox", { name: /Summary of issue/ })).toHaveCount(3);
      await dialog.locator('[data-action="create-issues"]').click();
      await expect(dialog.locator("[data-result]")).toHaveCount(3);
      await expect(dialog.locator('[data-result="created"]')).toHaveText(["CP-6 Write the migration guide"]);
      await expect(dialog.locator('[data-result="failed"]')).toContainText("Update the status page");
      await expect(dialog.locator('[data-result="failed"]')).toContainText("Armature refuses this summary.");
      await expect(dialog.locator('[data-result="skipped"]')).toContainText("Tell the support team");
      await expectAccessible(page);
      await dialog.locator('[data-action="close-created"]').click();
      await expect(box.locator("li p", { has: chip(page, "CP-6") })).toContainText("Write the migration guide");
      await expect(box.locator("li p", { hasText: "Update the status page" }).locator("[data-armature-issue]")).toHaveCount(0);

      await publishFromEditor(page);
      await expect(chip(page.locator("[data-doc]"), "CP-6")).toBeVisible();
    });

    test(`alice files table rows as issues, and Armature links each back to the page, in ${scheme}`, async ({
      page,
      api,
      freshOrg,
      request,
    }, testInfo) => {
      const { space, target, box, dialog } = await openInEditor(page, api, testInfo, scheme);

      // Each row is named by its first cell with text; the header row is left out.
      await box.locator("td", { hasText: "Archive the old logs" }).click();
      await box.locator("td", { hasText: "Rotate the backup keys" }).click({ modifiers: ["Shift"] });
      await expect(createButton(page)).toHaveText("Create 2 Armature issues");
      await createButton(page).click();
      await expect(dialog.getByRole("textbox", { name: "Summary of issue 1" })).toHaveValue("Archive the old logs");
      await expect(dialog.getByRole("textbox", { name: "Summary of issue 2" })).toHaveValue("Rotate the backup keys");
      await dialog.locator('[data-action="create-issues"]').click();
      await expect(dialog.locator('[data-result="created"]')).toHaveText(["CP-6 Archive the old logs", "CP-7 Rotate the backup keys"]);
      await dialog.locator('[data-action="close-created"]').click();
      await expect(box.locator("td", { has: chip(page, "CP-6") })).toContainText("Archive the old logs");
      await expect(box.locator("td", { has: chip(page, "CP-7") })).toContainText("Rotate the backup keys");

      await publishFromEditor(page);
      const doc = page.locator("[data-doc]");
      for (const key of ["CP-6", "CP-7"]) await expect(chip(doc, key)).toBeVisible();
      await expectAccessible(page);

      // Armature holds them as filed by alice's Armature self, each linking back to the page.
      const filed = (await stub(request, "GET", `${freshOrg.slug}/issues/CP-7`)) as { issue: { reporter: { name: string }; description: unknown } };
      expect(filed.issue.reporter.name).toBe("Admin");
      expect(JSON.stringify(filed.issue.description)).toContain(`${WEB_URL}/s/${space.key}/p/${target.id}`);
      expect(JSON.stringify(filed.issue.description)).toContain(target.title);
    });
  }

  test("nothing is offered without a token", async ({ page, api }, testInfo) => {
    await api.DELETE("/armature/account/token");
    const space = await freshSpace(api, testInfo);
    const target = await createPage(api, space.homePageId, uniqueName(testInfo, "Review notes"), actionItems);
    await openPage(page, space.key, target);
    await page.locator('[data-action="edit-page"]').click();
    const box = page.locator("#page-body");
    await selectLine(box, "Renew the TLS certificate");
    await expect(createButton(page)).toHaveCount(0);
  });
});
