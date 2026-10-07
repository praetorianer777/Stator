import type { Page, TestInfo } from "@playwright/test";
import { must, type StatorApi } from "../fixtures/api";
import { expect } from "../fixtures/auth";
import { orgTest as test } from "../fixtures/org";
import { ONE_LOOK, openShowing, openUntil } from "../fixtures/replica";
import { uniqueName } from "../fixtures/seed";
import { expectAccessible, scrollsSideways, startInScheme, type ColourScheme } from "../fixtures/shell";
import { createPage, createSpace, deleteSpace, publishFromEditor, uniqueKey, type Space } from "../fixtures/spaces";

const dialog = (page: Page) => page.locator("[data-new-page-dialog]");
const option = (page: Page, name: string) => dialog(page).getByRole("radio", { name: new RegExp(`^${name}`) });
const valuesForm = (page: Page) => dialog(page).getByRole("group", { name: "Fill in the template" });

/** A template of the space with a required text, a choice and a person, made through the API. */
async function kickoffIn(api: StatorApi, space: Space, name: string) {
  const blank = (variable: string) => ({ type: "templateVariable", attrs: { name: variable } });
  return must(
    await api.POST("/templates", {
      body: {
        spaceKey: space.key,
        name,
        description: "Who we start with.",
        title: "Kickoff with {customer}",
        body: {
          type: "doc",
          content: [
            { type: "paragraph", content: [{ type: "text", text: "Customer: " }, blank("customer")] },
            { type: "paragraph", content: [{ type: "text", text: "Stage: " }, blank("stage")] },
            { type: "paragraph", content: [{ type: "text", text: "Notes: " }, blank("notes")] },
          ],
        },
        variables: [
          { name: "customer", label: "Customer", kind: "text", options: [], default: "", required: true },
          { name: "stage", label: "Stage", kind: "select", options: ["Lead", "Won"], default: "Lead", required: false },
          { name: "notes", label: "Anything else", kind: "text", options: [], default: "", required: false },
        ],
      },
    }),
  ).template;
}

/** Opens the new page dialog under the space's home page once the template is offered, which a replica may still be catching up on. */
async function openNewPage(page: Page, space: Space, template: string) {
  await openUntil(page, `/s/${space.key}`, async () => {
    await page.locator('[data-action="new-page"]').click();
    await expect(option(page, template)).toBeVisible(ONE_LOOK);
  });
}

test.describe("templates with variables", { tag: ["@auth", "@desktop"] }, () => {
  const made: string[] = [];
  test.afterEach(async ({ api }) => {
    for (const key of made.splice(0)) await deleteSpace(api, key);
  });

  async function freshSpace(api: StatorApi, testInfo: TestInfo, label: string) {
    const key = uniqueKey(testInfo);
    made.push(key);
    return createSpace(api, key, uniqueName(testInfo, label));
  }

  test("an administrator adds variables to a template, and a member fills them in for a page that shows the values", async ({
    page,
    api,
    apiAs,
    pageAs,
  }, testInfo) => {
    test.slow();
    const space = await freshSpace(api, testInfo, "Kickoffs");
    const alice = must(await api.GET("/auth/me")).user;
    await page.goto(`/s/${space.key}/settings?tab=templates`);
    await page.locator('[data-action="new-template"]').click();
    await expect(page).toHaveURL(/\/settings\/templates\/new\?space=/);
    await page.getByLabel("Name", { exact: true }).fill("Kickoff");
    await page.getByLabel("Page title", { exact: true }).fill("Kickoff with {customer}");
    const variables = page.locator("[data-template-variables]");
    await page.locator('[data-action="add-variable"]').click();
    const first = variables.getByRole("listitem").nth(0);
    await first.getByLabel("Label", { exact: true }).fill("Customer");
    await expect(first.getByLabel("Name", { exact: true })).toHaveValue("customer");
    await first.getByLabel("Required").check();
    await page.locator('[data-action="add-variable"]').click();
    const second = variables.getByRole("listitem").nth(1);
    await second.getByLabel("Label", { exact: true }).fill("Owner");
    await second.getByLabel("Kind", { exact: true }).selectOption("person");
    const body = page.locator("#template-body");
    await body.click();
    await page.keyboard.type("Customer: ");
    await first.getByRole("button", { name: "Insert Customer" }).click();
    await expect(body.locator('[data-template-variable="customer"]')).toBeVisible();
    await page.keyboard.press("Enter");
    await page.keyboard.type("Owner: ");
    await second.getByRole("button", { name: "Insert Owner" }).click();
    await expect(body.locator('[data-template-variable="owner"]')).toBeVisible();
    await page.locator('[data-action="save-template"]').click();
    await expect(page).toHaveURL(new RegExp(`/s/${space.key}/settings\\?tab=templates$`));
    await expect(page.locator('[data-template-row="Kickoff"]')).toContainText("2 variables");

    const bob = await pageAs("bob");
    await openNewPage(bob, space, "Kickoff");
    await option(bob, "Kickoff").click();
    await expect(dialog(bob).locator("[data-template-preview] [data-template-variable=customer]")).toBeVisible();
    await valuesForm(bob).getByLabel("Customer (required)", { exact: true }).fill("Acme");
    await valuesForm(bob).getByRole("combobox", { name: "Owner" }).fill(alice.name.slice(0, 3));
    await valuesForm(bob)
      .getByRole("option", { name: new RegExp(alice.name) })
      .click();
    await expect(valuesForm(bob).locator(`[data-template-person="${alice.name}"]`)).toBeVisible();
    await dialog(bob).locator('[data-action="confirm-new-page"]').click();
    await expect(bob).toHaveURL(/\/p\/[0-9a-f-]+\/kickoff-with-acme\/edit$/);
    await expect(bob.locator("#page-body")).toContainText("Customer: Acme");
    await publishFromEditor(bob);
    const doc = bob.locator("[data-doc]");
    await expect(doc).toContainText("Customer: Acme");
    await expect(doc.locator("[data-mention]")).toHaveText(`@${alice.name}`);
    await expect(doc.locator("[data-template-variable]")).toHaveCount(0);
    await expect(bob.getByRole("heading", { level: 1 })).toHaveText("Kickoff with Acme");

    const bobApi = await apiAs("bob");
    const refused = await bobApi.POST("/templates", {
      body: { spaceKey: space.key, name: "Mine", description: "", title: "", body: { type: "doc", content: [] }, variables: [] },
    });
    expect(refused.response.status).toBe(403);
  });

  test("the form is filled from the keyboard alone, and an empty optional value leaves nothing behind", async ({ page, api }, testInfo) => {
    const space = await freshSpace(api, testInfo, "Keys");
    await kickoffIn(api, space, "Kickoff");
    await openNewPage(page, space, "Kickoff");
    await option(page, "Blank page").focus();
    await page.keyboard.press("ArrowDown");
    await expect(option(page, "Kickoff")).toBeFocused();
    await expect(option(page, "Kickoff")).toHaveAttribute("aria-checked", "true");
    await expect(dialog(page).getByLabel("Title", { exact: true })).toHaveValue("Kickoff with {customer}");
    await page.keyboard.press("Tab");
    await page.keyboard.press("Tab");
    await expect(valuesForm(page).getByLabel("Customer (required)", { exact: true })).toBeFocused();
    await page.keyboard.type("Beta");
    await page.keyboard.press("Enter");
    await expect(page).toHaveURL(/\/p\/[0-9a-f-]+\/kickoff-with-beta\/edit$/);
    await expect(page.locator("#page-body [data-hint]")).toHaveText("Anything else");
    await publishFromEditor(page);
    const doc = page.locator("[data-doc]");
    await expect(doc).toContainText("Customer: Beta");
    await expect(doc).toContainText("Stage: Lead");
    await expect(doc).not.toContainText("Anything else");
  });

  test("a template button asks for the template's variables, then opens the page with their values", async ({ page, api }, testInfo) => {
    const space = await freshSpace(api, testInfo, "Buttons");
    const template = await kickoffIn(api, space, "Kickoff");
    const board = await createPage(api, space.homePageId, "Customers", {
      type: "doc",
      content: [{ type: "templateButton", attrs: { template: template.key, space: null, parent: null, label: "New kickoff", title: "" } }],
    });
    await openShowing(page, `/s/${space.key}/p/${board.id}/customers`, page.locator('main [data-template-button] [data-action="create-from-template"]'));
    const button = page.locator("main [data-template-button]").getByRole("button", { name: "New kickoff" });
    await expect(button).toBeEnabled();
    await expect(button).toHaveAccessibleDescription(/^Asks what Kickoff needs/);
    await button.click();
    const asking = page.getByRole("dialog", { name: "New page from Kickoff" });
    await asking.getByRole("button", { name: "Create page" }).click();
    await expect(asking.getByRole("alert")).toHaveText("Fill in Customer first; the template needs it.");
    await asking.getByLabel("Customer (required)", { exact: true }).fill("Gamma");
    await asking.getByLabel("Stage", { exact: true }).selectOption("Won");
    await expectAccessible(page);
    await asking.getByRole("button", { name: "Create page" }).click();
    await expect(page).toHaveURL(/\/p\/[0-9a-f-]+\/kickoff-with-gamma\/edit$/);
    await expect(page.locator("#page-body")).toContainText("Customer: Gamma");
    await expect(page.locator("#page-body")).toContainText("Stage: Won");
  });

  for (const scheme of ["light", "dark"] as ColourScheme[]) {
    test(`the template editor and the form pass axe in ${scheme}`, async ({ page, api }, testInfo) => {
      const space = await freshSpace(api, testInfo, "Axe");
      const template = await kickoffIn(api, space, "Kickoff");
      await startInScheme(page, scheme);
      await openShowing(page, `/settings/templates/${template.key}`, page.locator("#template-body [data-template-variable=customer]"));
      await expect(page.locator("html")).toHaveAttribute("data-theme", scheme);
      await expectAccessible(page);
      await openNewPage(page, space, "Kickoff");
      await option(page, "Kickoff").click();
      await expect(valuesForm(page)).toBeVisible();
      await expectAccessible(page);
    });
  }
});

// Its own group: a test tagged for both sizes would be left out of both projects.
test.describe("templates with variables on a phone", { tag: ["@auth", "@mobile"] }, () => {
  const made: string[] = [];
  test.afterEach(async ({ api }) => {
    for (const key of made.splice(0)) await deleteSpace(api, key);
  });

  test("the template editor and the form fit and never scroll sideways", async ({ page, api }, testInfo) => {
    const key = uniqueKey(testInfo);
    made.push(key);
    const space = await createSpace(api, key, uniqueName(testInfo, "Phone"));
    const template = await kickoffIn(api, space, "Kickoff");
    await openShowing(page, `/settings/templates/${template.key}`, page.locator("[data-template-variables]"));
    await expect(page.locator('[data-action="save-template"]')).toBeVisible();
    expect(await scrollsSideways(page)).toBe(false);
    await openNewPage(page, space, "Kickoff");
    await option(page, "Kickoff").click();
    await expect(valuesForm(page).getByLabel("Customer (required)", { exact: true })).toBeVisible();
    expect(await scrollsSideways(page)).toBe(false);
  });
});
