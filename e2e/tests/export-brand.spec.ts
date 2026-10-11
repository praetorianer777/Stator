import { expect } from "../fixtures/auth";
import { orgTest as test } from "../fixtures/org";
import { expectAccessible } from "../fixtures/shell";

// A 1 by 1 PNG, which is all a logo needs to be for the page to draw it.
const LOGO = Buffer.from("iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mNkYPhfDwAChwGA60e6kgAAAABJRU5ErkJggg==", "base64");

test.describe("the brand of the exports", { tag: ["@auth", "@desktop"] }, () => {
  test("an administrator sets a logo and footer lines, the sidebar shows the logo to everybody, and a member cannot change them", async ({ page, pageAs }) => {
    await page.goto("/settings/brand");
    await expect(page.getByText("There is no logo yet.", { exact: false })).toBeVisible();
    await expectAccessible(page);

    await page.locator("[data-brand-logo-input]").setInputFiles({ name: "logo.png", mimeType: "image/png", buffer: LOGO });
    await expect(page.locator("[data-brand-logo]")).toBeVisible();
    await page.getByLabel("Footer line in English").fill("Internal use only");
    await page.getByLabel("Footer line in German").fill("Nur für den internen Gebrauch");
    await page.getByRole("button", { name: "Save footer" }).click();
    await expect(page.getByRole("button", { name: "Save footer" })).toBeEnabled();

    await page.reload();
    await expect(page.getByLabel("Footer line in English")).toHaveValue("Internal use only");
    await expect(page.getByLabel("Footer line in German")).toHaveValue("Nur für den internen Gebrauch");
    await expect(page.locator("[data-sidebar] [data-org-logo]")).toBeVisible();
    await expect(page.locator("[data-sidebar] [data-logo-mark]")).toHaveCount(0);

    const bob = await pageAs("bob");
    await bob.goto("/spaces");
    await expect(bob.locator("[data-sidebar] [data-org-logo]")).toBeVisible();
    await bob.goto("/settings/brand");
    await expect(bob.getByText("Only an administrator of the organization changes the brand", { exact: false })).toBeVisible();

    await page.getByRole("button", { name: "Remove logo" }).click();
    await expect(page.getByText("There is no logo yet.", { exact: false })).toBeVisible();
    await page.goto("/spaces");
    await expect(page.locator("[data-sidebar] [data-logo-mark]")).toBeVisible();
  });

  test("a logo that is not a picture is refused in a sentence", async ({ page }) => {
    await page.goto("/settings/brand");
    await page.locator("[data-brand-logo-input]").setInputFiles({ name: "logo.png", mimeType: "image/png", buffer: Buffer.from("not a picture") });
    await expect(page.getByText("a PNG, JPEG or WebP picture", { exact: false }).first()).toBeVisible();
  });
});
