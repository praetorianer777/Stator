import type { APIRequestContext, Page, TestInfo } from "@playwright/test";
import { must, type StatorApi } from "../fixtures/api";
import { expect } from "../fixtures/auth";
import { caretTo } from "../fixtures/editor";
import { teamTest as test } from "../fixtures/team";
import { openPage, openShowing } from "../fixtures/replica";
import { uniqueName } from "../fixtures/seed";
import { expectAccessible } from "../fixtures/shell";
import { createPage, createSpace, deleteSpace, uniqueKey } from "../fixtures/spaces";
import { apiFromStubURL, exciterURL } from "../fixtures/stack";

const SETTINGS_PATH = "/settings/exciter";
const PROFILE_PATH = "/settings/profile";
// The stub makes a person of any name on first use; each spec's tenant is its
// own. alice may still find a trashed requirement, as an administrator does.
const patFor = (tenant: string, person: string) => `exciter_pat_${tenant}_${person}`;
const SECRET = "exciter_whs_abcdefghijklmnopqrstuvwxyz0123456789ABCDEFG";
// What exciter-stub names itself at /meta.
const STUB_VERSION = "0.0.0+stub";
const BASKET = "The basket keeps its items for 30 days";

type Body = Parameters<typeof createPage>[3];
const chips = (...keys: string[]): Body =>
  ({
    type: "doc",
    content: [
      {
        type: "paragraph",
        content: keys.flatMap((key) => [
          { type: "exciterRequirement", attrs: { key } },
          { type: "text", text: " " },
        ]),
      },
    ],
  }) as Body;
const chip = (page: Page, key: string) => page.locator(`main [data-doc] [data-exciter-requirement="${key}"]`);
const requirementAt = (key: string, project = "CHK") => `${exciterURL()}/p/${project}/r/${key}`;

async function stub(request: APIRequestContext, method: "POST" | "PUT" | "DELETE", path: string, data?: unknown) {
  const answer = await request.fetch(`${exciterURL()}/_stub/${path}`, { method, data });
  expect(answer.ok(), `the stub answered ${method} ${path} with ${answer.status()}`).toBe(true);
  return answer.status() === 204 ? null : ((await answer.json()) as Record<string, unknown>);
}

test.describe("Exciter requirements on pages", { tag: ["@auth"] }, () => {
  const made: string[] = [];

  test.beforeEach(async ({ api, team, request }) => {
    await stub(request, "DELETE", team.label);
    await api.DELETE("/exciter/connection");
  });

  test.afterEach(async ({ api }) => {
    for (const key of made.splice(0)) await deleteSpace(api, key);
    await api.DELETE("/exciter/connection");
  });

  async function freshPage(api: StatorApi, testInfo: TestInfo, body: Body) {
    const key = uniqueKey(testInfo);
    made.push(key);
    const space = await createSpace(api, key, uniqueName(testInfo, "Requirements"));
    return { space, target: await createPage(api, space.homePageId, uniqueName(testInfo, "Checkout"), body) };
  }

  test("an administrator connects Exciter, and alice connects her account from a chip", async ({ page, api, team }, testInfo) => {
    await page.goto("/");
    await page.locator('[data-action="account"]').click();
    await page.getByRole("menu", { name: "Your account" }).locator('[data-action="exciter-settings"]').click();
    await expect(page).toHaveURL(new RegExp(`${SETTINGS_PATH}$`));
    await expect(page.locator("main").getByRole("heading", { level: 1, name: "Exciter" })).toBeVisible();

    const address = page.getByLabel("Exciter address", { exact: true });
    await address.fill("https://10.11.12.13");
    await page.locator('[data-action="check-exciter-address"]').click();
    await expect(page.getByText("Use Exciter's public address, or add 10.11.12.13 to network.outboundAllow")).toBeVisible();
    await address.fill(exciterURL());
    await page.locator('[data-action="check-exciter-address"]').click();
    await expect(page.locator('[data-exciter-check="found"]')).toHaveText(`Exciter ${STUB_VERSION} answers at this address.`);
    await page.getByLabel("Webhook secret", { exact: true }).fill(SECRET);
    await page.locator('[data-action="save-exciter"]').click();
    await expect(page.getByText("Saved.", { exact: true })).toBeVisible();
    await expect(page.getByLabel("Webhook address", { exact: true })).toHaveValue(/\/api\/v1\/exciter\/webhook$/);
    await expect(page.locator("[data-exciter-capabilities]")).toHaveAttribute("data-exciter-capabilities", "all");
    await expect(page.locator("body")).not.toContainText(SECRET);
    await expectAccessible(page);

    const { space, target } = await freshPage(api, testInfo, chips("CHK-R1", "OLD-R7", "CHK-R99"));
    await openPage(page, space.key, target);
    await expect(chip(page, "CHK-R1")).toHaveAttribute("data-state", "connect");
    await expect(chip(page, "CHK-R1").getByRole("link", { name: "CHK-R1" })).toHaveAttribute("href", requirementAt("CHK-R1"));
    await expectAccessible(page);
    await chip(page, "CHK-R1").getByRole("link", { name: "Connect Exciter to see this requirement" }).click();
    await expect(page).toHaveURL(new RegExp(`${PROFILE_PATH}\\?return=.*#exciter$`));
    const token = page.getByLabel("Exciter token", { exact: true });
    await expect(token).toBeFocused();
    await token.fill(patFor(team.label, "alice"));
    await page.locator('[data-action="connect-exciter"]').click();

    await expect(page.locator("[data-armature-returned]")).toHaveText(/Your Exciter account is connected/);
    const basket = chip(page, "CHK-R1");
    await expect(basket).toHaveAttribute("data-state", "requirement");
    await expect(basket).toHaveAttribute("href", requirementAt("CHK-R1"));
    await expect(basket).toContainText(BASKET);
    await expect(basket.locator("[data-status-category]")).toHaveText(/approved/i);
    await expect(basket.locator("[data-revision]")).toHaveAttribute("data-revision", "3");
    // A key the requirement had before shows the one it has now.
    await expect(chip(page, "OLD-R7")).toContainText("CHK-R4");
    await expect(chip(page, "OLD-R7")).toHaveAttribute("href", requirementAt("CHK-R4"));
    await expect(chip(page, "CHK-R99")).toHaveAttribute("data-state", "hidden");
    await expectAccessible(page);
  });

  test("each reader sees what their own token shows, and a change as soon as Exciter announces it", async ({
    page,
    api,
    apiAs,
    pageAs,
    team,
    request,
  }, testInfo) => {
    must(await api.PUT("/exciter/connection", { body: { baseUrl: exciterURL(), webhookSecret: SECRET } }));
    must(await api.PUT("/exciter/account/token", { body: { token: patFor(team.label, "alice") } }));
    const { space, target } = await freshPage(api, testInfo, chips("CHK-R1", "CHK-R2"));

    await openPage(page, space.key, target);
    await expect(chip(page, "CHK-R2")).toHaveAttribute("data-state", "requirement");
    await expect(chip(page, "CHK-R1")).toContainText(BASKET);

    // Exciter answers bob 404 for CHK-R2, so his reading of the same page shows him only its key.
    must(await (await apiAs("bob")).PUT("/exciter/account/token", { body: { token: patFor(team.label, "bob") } }));
    const bob = await pageAs("bob");
    await openShowing(bob, `/s/${space.key}/p/${target.id}/page`, BASKET);
    await expect(chip(bob, "CHK-R2")).toHaveAttribute("data-state", "hidden");
    await expect(chip(bob, "CHK-R2")).not.toContainText("Payment providers");

    await stub(request, "PUT", `${team.label}/requirements/CHK-R1`, { title: "The basket keeps its items for 90 days", revision: 4 });
    // Within the cache's minute the page still shows what was read.
    await page.reload();
    await expect(chip(page, "CHK-R1")).toContainText(BASKET);
    const sent = await stub(request, "POST", `${team.label}/webhooks`, {
      url: `${apiFromStubURL()}/api/v1/exciter/webhook`,
      secret: SECRET,
      topic: "requirement.revised",
      key: "CHK-R1",
    });
    expect(sent?.status).toBe(204);
    await page.reload();
    await expect(chip(page, "CHK-R1")).toContainText("The basket keeps its items for 90 days");
    await expect(chip(page, "CHK-R1").locator("[data-revision]")).toHaveAttribute("data-revision", "4");

    const forged = await stub(request, "POST", `${team.label}/webhooks`, {
      url: `${apiFromStubURL()}/api/v1/exciter/webhook`,
      secret: `${SECRET.slice(0, -1)}H`,
      topic: "requirement.revised",
      key: "CHK-R1",
    });
    expect(forged?.status).toBe(401);
    const settings = must(await api.GET("/exciter/connection")).connection;
    expect(settings?.lastDelivery?.topic).toBe("requirement.revised");
  });

  test("the slash menu offers an Exciter requirement only while Stator is connected to Exciter", async ({ page, api, team }, testInfo) => {
    const { space, target } = await freshPage(api, testInfo, {
      type: "doc",
      content: [{ type: "paragraph", content: [{ type: "text", text: "Scope." }] }, { type: "paragraph" }],
    } as Body);
    const path = `/s/${space.key}/p/${target.id}/page`;
    const box = page.locator("#page-body");
    const menu = page.getByRole("listbox", { name: "Insert a block" });

    await openShowing(page, `${path}/edit`, "Scope.");
    await caretTo(box, "end");
    await page.keyboard.type("/");
    await expect(menu).toBeVisible();
    await expect(menu.getByRole("option", { name: /Exciter requirement/ })).toHaveCount(0);
    await page.keyboard.press("Escape");
    await page.keyboard.press("Backspace");

    must(await api.PUT("/exciter/connection", { body: { baseUrl: exciterURL() } }));
    must(await api.PUT("/exciter/account/token", { body: { token: patFor(team.label, "alice") } }));
    await openShowing(page, `${path}/edit`, "Scope.");
    await caretTo(box, "end");
    await page.keyboard.type("/exciter");
    await expect(menu.getByRole("option", { name: /Exciter requirement/ })).toBeVisible();
    await page.keyboard.press("Enter");
    const dialog = page.getByRole("dialog", { name: "Insert an Exciter requirement" });
    await dialog.getByLabel("Requirement key").fill("chk-r");
    await dialog.getByRole("button", { name: "Insert" }).click();
    await expect(dialog).toContainText("That is not a requirement key.");
    await expectAccessible(page);
    await dialog.getByLabel("Requirement key").fill("chk-r1");
    await dialog.getByRole("button", { name: "Insert" }).click();
    await expect(box.locator('[data-exciter-requirement="CHK-R1"]')).toContainText(BASKET);

    // A key typed by an author Exciter answers becomes a chip too.
    await page.keyboard.type("and CHK-R4 ");
    await expect(box.locator('[data-exciter-requirement="CHK-R4"]')).toContainText("An order is confirmed by mail");
  });
});
