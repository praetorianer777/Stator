import { expect, type Locator, type Page } from "@playwright/test";

// Read-your-writes holds a session to its own writes only. A read by anybody
// else, bob, an anonymous reader, a token, a session signed in afresh, or a
// read after a write by the Armature stub or by a job, may be answered by a
// replica that has not replayed that write. Such a read retries until it sees
// what only the newest write shows: its words, its row, the count it raised,
// or the absence a restriction, removal or revocation leaves, and an absence
// only once the page has shown it is loaded. A read may reach the primary once
// and a replica behind it the next time, so an earlier retry that saw the
// write does not cover a later read of it. A page read goes through
// openShowing, openWithout or openUntil; an API read through expect.poll or
// expect(...).toPass() on that same state. Never raise a timeout to cover the
// lag, and never wait for a state an older answer shows too, or the wait would
// pass with the feature broken. withDatabase waits for the replicas itself.

/** How long one look waits before the page is loaded again. */
export const ONE_LOOK = { timeout: 2_000 } as const;

/** Loads path until check passes; check's own waits take ONE_LOOK.
 *  Another session's write may reach this read a replica later. */
export async function openUntil(page: Page, path: string, check: () => Promise<unknown>): Promise<void> {
  await expect(async () => {
    await page.goto(path);
    await check();
  }).toPass();
}

/** Loads path until it shows `shown`: words within main, or a locator made visible.
 *  Another session's write may reach this read a replica later. */
export async function openShowing(page: Page, path: string, shown: string | Locator): Promise<void> {
  await openUntil(page, path, () =>
    typeof shown === "string" ? expect(page.locator("main")).toContainText(shown, ONE_LOOK) : expect(shown).toBeVisible(ONE_LOOK),
  );
}

/** Loads path until `loaded` is visible and `gone` matches nothing, as after a restriction or removal.
 *  `loaded` proves the answer came, since a page still loading shows nothing either. */
export async function openWithout(page: Page, path: string, gone: Locator, loaded: Locator): Promise<void> {
  await openUntil(page, path, async () => {
    await expect(loaded).toBeVisible(ONE_LOOK);
    await expect(gone).toHaveCount(0, ONE_LOOK);
  });
}

/** Opens a wiki page by id until its title shows as the heading, and `check` passes when given.
 *  The page, or what check looks for, may be another session's write. */
export async function openPage(page: Page, spaceKey: string, target: { id: string; title: string }, check?: () => Promise<unknown>): Promise<void> {
  await openUntil(page, `/s/${spaceKey}/p/${target.id}/page`, async () => {
    await expect(page.locator("main").getByRole("heading", { level: 1, name: target.title, exact: true })).toBeVisible(ONE_LOOK);
    await check?.();
  });
}

/** Opens the editor of the page at path until it holds the words given.
 *  The words may be another session's write. */
export async function openEditor(page: Page, path: string, words: string): Promise<void> {
  await openUntil(page, `${path}/edit`, () => expect(page.locator("#page-body")).toContainText(words, ONE_LOOK));
}
