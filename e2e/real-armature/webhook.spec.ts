import { expect } from "../fixtures/auth";
import { openPage } from "../fixtures/replica";
import { uniqueName } from "../fixtures/seed";
import { createPage, createSpace, deleteSpace, uniqueKey } from "../fixtures/spaces";
import { teamTest as test } from "../fixtures/team";
import { answered, armatureAs, armatureSignedIn, connectAccount, connectSite, readSeed } from "./armature";

// Armature's own worker posts the signed webhook the seed registered to the
// stack's api, which clears what it caches of the issue; nothing here sends
// a delivery itself.

type Body = Parameters<typeof createPage>[3];

/** Well inside the minute an issue stays cached, so only the webhook can explain a change seen this soon. */
const SOONER_THAN_THE_CACHE = { timeout: 20_000 };
/** The one envelope version Stator's receiver reads, armature.WebhookVersion. */
const ENVELOPE_VERSION = 1;

test.describe("a real Armature's webhooks", { tag: ["@auth"] }, () => {
  let spaceKey = "";

  test.beforeEach(async ({ api }) => {
    const seed = readSeed();
    await connectSite(api, seed, true);
    await connectAccount(api, seed.people.member);
  });

  test.afterEach(async ({ api }) => {
    if (spaceKey) await deleteSpace(api, spaceKey);
    await api.DELETE("/armature/connection");
  });

  test("a change made in Armature reaches the page through the signed webhook", async ({ page, api }, testInfo) => {
    const seed = readSeed();
    const ada = await armatureAs(seed.people.admin.token);
    try {
      const before = uniqueName(testInfo, "Announce the wiki");
      const after = `${before}, in the newsletter`;
      const { issue } = await answered<{ issue: { key: string } }>(
        ada.post("issues", { data: { projectKey: seed.project.key, summary: before } }),
        201,
        "an issue to change",
      );

      spaceKey = uniqueKey(testInfo);
      const space = await createSpace(api, spaceKey, uniqueName(testInfo, "Hooks"));
      const target = await createPage(api, space.homePageId, uniqueName(testInfo, "Announcement"), {
        type: "doc",
        content: [
          {
            type: "paragraph",
            content: [
              { type: "text", text: "Tracked in " },
              { type: "armatureIssue", attrs: { key: issue.key } },
            ],
          },
        ],
      } as Body);
      await openPage(page, space.key, target);
      const chip = page.locator(`[data-doc] [data-armature-issue="${issue.key}"]`);
      await expect(chip).toContainText(before);

      await answered(ada.patch(`issues/${issue.key}`, { data: { summary: after } }), 200, "rename the issue");
      await expect(async () => {
        await page.reload();
        await expect(chip).toContainText(after, { timeout: 2_000 });
      }).toPass(SOONER_THAN_THE_CACHE);

      // Armature's log of the endpoint says Stator took the delivery.
      const admin = await armatureSignedIn(seed.people.admin.email);
      const { webhooks } = await answered<{ webhooks: Array<{ id: string; name: string }> }>(admin.get("webhooks"), 200, "the webhooks");
      const hook = webhooks.find((each) => each.name === "Stator");
      expect(hook, "the seed's webhook to Stator").toBeDefined();
      await expect
        .poll(async () => {
          const { deliveries } = await answered<{ deliveries: Array<{ topic: string; status: number | null }> }>(
            admin.get(`webhooks/${hook!.id}/deliveries`),
            200,
            "the deliveries",
          );
          return deliveries.filter((each) => each.topic === "issue.updated").map((each) => each.status);
        })
        .toContain(204);
      await admin.dispose();

      // What Stator took was Armature's numbered envelope, signed with the
      // time it was sent, which the receiver held to its window (#412).
      const { data } = await api.GET("/armature/connection");
      expect(data?.connection?.lastDelivery, "the last delivery Stator took").toMatchObject({
        version: ENVELOPE_VERSION,
        timestamped: true,
      });
      await page.goto("/settings/armature");
      await expect(page.locator("[data-last-delivery]")).toContainText(`in envelope version ${ENVELOPE_VERSION}, signed with the time it was sent.`);
    } finally {
      await ada.dispose();
    }
  });
});
