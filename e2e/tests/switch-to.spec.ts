import type { Page } from "@playwright/test";
import { must } from "../fixtures/api";
import { expect } from "../fixtures/auth";
import { createSpace, deleteSpace, uniqueKey } from "../fixtures/spaces";
import { uniqueName } from "../fixtures/seed";
import { openAccountMenu } from "../fixtures/shell";
import { armatureURL, exciterURL, WEB_URL } from "../fixtures/stack";
import { createPerson, removePerson, teamTest as test } from "../fixtures/team";

// The avatar menu's "Switch to" group (#494): a plain link to each tool the
// organization is connected to, for everyone signed in.

function switchTo(page: Page) {
  return page.getByRole("menu", { name: "Your account" }).getByRole("group", { name: "Switch to" });
}

async function openMenu(page: Page) {
  await page.goto("/");
  await openAccountMenu(page);
}

test.describe("switching to the connected tools", { tag: ["@auth"] }, () => {
  test.beforeEach(async ({ api }) => {
    await api.DELETE("/armature/connection");
    await api.DELETE("/exciter/connection");
  });

  test.afterEach(async ({ api }) => {
    await api.DELETE("/armature/connection");
    await api.DELETE("/exciter/connection");
  });

  test("with both connected the menu links to each, and a link opens that tool's home", async ({ page, api }) => {
    must(await api.PUT("/armature/connection", { body: { baseUrl: armatureURL() } }));
    must(await api.PUT("/exciter/connection", { body: { baseUrl: exciterURL() } }));

    await openMenu(page);
    const group = switchTo(page);
    await expect(group.getByRole("menuitem")).toHaveText(["Armature", "Exciter"]);
    await expect(group.getByRole("menuitem", { name: "Armature" })).toHaveAttribute("href", armatureURL());
    await expect(group.getByRole("menuitem", { name: "Exciter" })).toHaveAttribute("href", exciterURL());

    await group.getByRole("menuitem", { name: "Exciter" }).click();
    await expect(page).toHaveURL(new URL(exciterURL()).href);
  });

  test("with one connected only it is listed, to a guest as well", async ({ browser, api }, testInfo) => {
    must(await api.PUT("/exciter/connection", { body: { baseUrl: exciterURL() } }));
    const key = uniqueKey(testInfo);
    await createSpace(api, key, uniqueName(testInfo, "Visits"));
    const guest = await createPerson(`gina-${key}`, `Gina ${key}`, "guest", key);
    const context = await browser.newContext({ baseURL: WEB_URL, storageState: guest.session });
    try {
      const page = await context.newPage();
      await openMenu(page);
      await expect(switchTo(page).getByRole("menuitem")).toHaveText(["Exciter"]);
    } finally {
      await context.close();
      await removePerson(guest.id);
      await deleteSpace(api, key);
    }
  });

  test("with none connected the group is not shown", async ({ page, api }) => {
    must(await api.PUT("/armature/connection", { body: { baseUrl: armatureURL() } }));
    expect((await api.DELETE("/armature/connection")).response.status).toBe(204);

    await openMenu(page);
    await expect(page.getByRole("menuitem", { name: "Sign out" })).toBeVisible();
    await expect(switchTo(page)).toHaveCount(0);
  });
});
