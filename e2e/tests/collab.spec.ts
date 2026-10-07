import type { Page } from "@playwright/test";
import { must } from "../fixtures/api";
import { expect } from "../fixtures/auth";
import { caretTo } from "../fixtures/editor";
import { orgTest as test } from "../fixtures/org";
import { ONE_LOOK, openUntil } from "../fixtures/replica";
import { uniqueName } from "../fixtures/seed";
import { createPage, createSpace, deleteSpace, publishFromEditor, uniqueKey } from "../fixtures/spaces";

const doc = (...lines: string[]) => ({ type: "doc", content: lines.map((text) => ({ type: "paragraph", content: [{ type: "text", text }] })) });
const body = (page: Page) => page.locator("#page-body");
const presence = (page: Page) => page.locator("[data-presence]");

/** The body's words, without the names on the other people's carets, which sit among them. */
const words = (page: Page) =>
  body(page).evaluate((el) => {
    const copy = el.cloneNode(true) as HTMLElement;
    for (const caret of copy.querySelectorAll(".collaboration-carets__caret")) caret.remove();
    return copy.textContent ?? "";
  });

/** Types at the end of the body, where the other person's words do not get in the way. */
async function typeAtEnd(page: Page, text: string) {
  await caretTo(body(page), "end");
  await page.keyboard.type(text);
}

test.describe("editing a page together", { tag: ["@auth", "@desktop"] }, () => {
  const made: string[] = [];
  test.afterEach(async ({ api }) => {
    for (const key of made.splice(0)) await deleteSpace(api, key);
  });

  test("two people see each other's words and carets, words written offline are merged, and the result is published", async ({
    page,
    api,
    apiAs,
    pageAs,
  }, testInfo) => {
    const key = uniqueKey(testInfo);
    made.push(key);
    const space = await createSpace(api, key, uniqueName(testInfo, "Together"));
    const plans = await createPage(api, space.homePageId, "Plans", doc("First words."));
    const aliceName = must(await api.GET("/auth/me")).user.name;
    const bobName = must(await (await apiAs("bob")).GET("/auth/me")).user.name;
    const path = `/s/${space.key}/p/${plans.id}/plans/edit`;

    await page.goto(path);
    await expect(page.locator("[data-page-editor]")).toHaveAttribute("data-collab", "together");
    const bob = await pageAs("bob");
    await openUntil(bob, path, () => expect(bob.locator("[data-page-editor]")).toHaveAttribute("data-collab", "together", ONE_LOOK));
    await expect.poll(() => words(bob)).toContain("First words.");

    // Each sees the other in the header.
    await expect(page.getByRole("list", { name: `Editing now: ${bobName}` })).toBeVisible();
    await expect(bob.getByRole("list", { name: `Editing now: ${aliceName}` })).toBeVisible();

    // Each sees the other's words as they are typed, and where the other's caret is.
    await typeAtEnd(page, " Alice was here.");
    await expect.poll(() => words(bob)).toContain("First words. Alice was here.");
    await expect(bob.locator(".collaboration-carets__label", { hasText: aliceName })).toBeVisible();
    await typeAtEnd(bob, " Bob too.");
    await expect.poll(() => words(page)).toContain("Alice was here. Bob too.");
    await expect(page.locator(".collaboration-carets__label", { hasText: bobName })).toBeVisible();

    // Bob loses the connection and keeps writing; Alice writes meanwhile.
    await bob.context().setOffline(true);
    await expect(presence(bob)).toHaveAttribute("data-presence", "offline");
    await typeAtEnd(bob, " Written offline.");
    await caretTo(body(page), "start");
    await page.keyboard.type("Meanwhile. ");
    await expect.poll(() => words(page)).toMatch(/^Meanwhile\. /);
    expect(await words(page)).not.toContain("Written offline.");
    expect(await words(bob)).not.toContain("Meanwhile.");

    // Back online, both have both.
    await bob.context().setOffline(false);
    await expect(presence(bob)).toHaveAttribute("data-presence", "live");
    const merged = "Meanwhile. First words. Alice was here. Bob too. Written offline.";
    await expect.poll(() => words(page)).toBe(merged);
    await expect.poll(() => words(bob)).toBe(merged);

    // Alice publishes the shared draft as it stands.
    await publishFromEditor(page, "Written together.");
    await expect(page.locator("[data-doc]")).toContainText(merged);
    // Read as alice's API client, which another session's write reaches once the replica has it.
    const version = async () => must(await api.GET("/pages/{pageID}", { params: { path: { pageID: plans.id } } })).page.version;
    await expect.poll(version).toBe(2);

    // Bob carries on from her version, so his publish is no conflict.
    await typeAtEnd(bob, " After.");
    await publishFromEditor(bob);
    await expect(bob.locator("[data-doc]")).toContainText(`${merged} After.`);
    await expect.poll(version).toBe(3);
  });
});
