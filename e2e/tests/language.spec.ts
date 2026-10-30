import { type BrowserContext, expect, type Page, test } from "@playwright/test";
import { createPerson, type Person, removePerson } from "../fixtures/team";
import { expectAccessible, openShell } from "../fixtures/shell";

// The choice belongs to the person, so these run as somebody of their own:
// alice in German would be German in every other spec at the same time.

const PROFILE_PATH = "/settings/profile";
const TOKENS_PATH = "/settings/tokens";
const LANGUAGE_FIELD = { en: "Interface language", de: "Sprache der Oberfläche" };
const TIMEZONE_FIELD = { en: "Time zone", de: "Zeitzone" };
const SAVE = { en: "Save", de: "Speichern" };
const PROFILE_TITLE = { en: "Your profile", de: "Ihr Profil" };
/** A medium date as German writes it, day first and dotted: 02.11.2026. */
const GERMAN_DATE = /\b\d{2}\.\d{2}\.\d{4}\b/;

let person: Person;

async function signIn(context: BrowserContext): Promise<void> {
  await context.addCookies(person.session.cookies);
}

async function expectProfileIn(page: Page, language: "en" | "de"): Promise<void> {
  await expect(page.getByRole("heading", { level: 1, name: PROFILE_TITLE[language] })).toBeVisible();
  await expect(page.locator("html")).toHaveAttribute("lang", language);
}

test.describe("the interface language", { tag: "@desktop" }, () => {
  test.beforeEach(async ({ context }, testInfo) => {
    person = await createPerson(`language-${testInfo.workerIndex}`, "Language", "member");
    await signIn(context);
  });
  test.afterEach(async () => {
    if (person) await removePerson(person.id);
  });

  test("is chosen in the profile, shown at once, kept by the server and writes dates in German", async ({ page, browser }) => {
    await openShell(page, PROFILE_PATH);
    await expectProfileIn(page, "en");

    await page.getByLabel(LANGUAGE_FIELD.en, { exact: true }).selectOption("de");
    await page.getByRole("button", { name: SAVE.en, exact: true }).click();
    await expectProfileIn(page, "de");
    await expect(page.locator("[data-language-saved]")).toHaveText("Ihre Sprache und Zeitzone sind gespeichert.");
    await expect(page.getByRole("button", { name: "Ihr Konto" })).toBeVisible();
    await expectAccessible(page);

    await page.reload();
    await expectProfileIn(page, "de");
    await expect(page.getByLabel(LANGUAGE_FIELD.de, { exact: true })).toHaveValue("de");

    await page.getByLabel(`Suchen: ${TIMEZONE_FIELD.de}`, { exact: true }).fill("Tokyo");
    await page.getByLabel(TIMEZONE_FIELD.de, { exact: true }).selectOption("Asia/Tokyo");
    await page.getByRole("button", { name: SAVE.de, exact: true }).click();
    await expect(page.locator("[data-language-saved]")).toHaveText("Ihre Sprache und Zeitzone sind gespeichert.");
    await page.reload();
    await expect(page.getByLabel(TIMEZONE_FIELD.de, { exact: true })).toHaveValue("Asia/Tokyo");

    await openShell(page, TOKENS_PATH);
    await expect(page.getByRole("heading", { level: 1, name: "Persönliche Zugriffstokens" })).toBeVisible();
    await page.getByLabel("Name des Tokens", { exact: true }).fill("Sprachprobe");
    await page.locator('[data-action="create-token"]').click();
    await page.locator('[data-action="token-done"]').click();
    await expect(page.locator('[data-token-row="Sprachprobe"] [data-token-expires]')).toHaveText(GERMAN_DATE);
    await expectAccessible(page);

    // A browser that never saw the choice gets it from the server.
    const elsewhere = await browser.newContext({ locale: "en-US" });
    try {
      await signIn(elsewhere);
      const other = await elsewhere.newPage();
      await openShell(other, PROFILE_PATH);
      await expectProfileIn(other, "de");
    } finally {
      await elsewhere.close();
    }
  });

  test.describe("in a German browser", () => {
    test.use({ locale: "de-DE" });

    test("starts in German, and a choice of English wins over it", async ({ page }) => {
      await openShell(page, PROFILE_PATH);
      await expectProfileIn(page, "de");
      const field = page.getByLabel(LANGUAGE_FIELD.de, { exact: true });
      await expect(field).toHaveValue("");
      await expect(field.locator("option:checked")).toHaveText("Wie der Browser (derzeit Deutsch)");
      await expectAccessible(page);

      await field.selectOption("en");
      await page.getByRole("button", { name: SAVE.de, exact: true }).click();
      await expectProfileIn(page, "en");
      await expectAccessible(page);

      await page.reload();
      await expectProfileIn(page, "en");
    });
  });
});
