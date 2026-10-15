import { writeFileSync } from "node:fs";
import { test as setup, request, type APIRequestContext } from "@playwright/test";
import { answered, passwordOf, realArmatureURL, SEED_FILE, WEBHOOK_TOPICS, WEBHOOK_URL, type ArmaturePerson, type Seed, type SeededIssue } from "./armature";

// Makes in the real Armature what the specs read, through Armature's own API
// as its people would: the first sign-up sets the installation up and
// administers it, later ones join as members, and every member is a user of
// every project until a grant is taken away. Run again against the same
// Armature (KEEP_ARMATURE=1), it finds what it made before.

const PROJECT = { key: "RA", name: "Real Armature" };
const TEAM = "Platform";
const PEOPLE = {
  admin: { name: "Ada Admin", email: "ada@armature.test" },
  member: { name: "Mia Member", email: "mia@armature.test" },
  outsider: { name: "Nils Outsider", email: "nils@armature.test" },
} as const;
const THEME_EXAMPLE = "deep-tech";
const WEBHOOK_NAME = "Stator";

type Who = keyof typeof PEOPLE;
type Session = { context: APIRequestContext; id: string };

const day = (date: Date) => date.toISOString().slice(0, 10);

/** Days in this month, so a calendar of the month shows every dated issue. */
function datesThisMonth(): Seed["dates"] {
  const now = new Date();
  const on = (n: number) => day(new Date(Date.UTC(now.getUTCFullYear(), now.getUTCMonth(), n)));
  return { guideStart: on(5), guideDue: on(16), runbooksStart: on(19), runbooksDue: on(27), signInDue: on(22) };
}

/** Signs up, or signs in when an earlier run made the account. */
async function sessionOf(who: Who): Promise<Session> {
  const { name, email } = PEOPLE[who];
  const context = await request.newContext({ baseURL: `${realArmatureURL()}/api/v1/` });
  const password = passwordOf(email);
  let res = await context.post("auth/signup", { data: { email, password, name } });
  if (!res.ok()) res = await context.post("auth/login", { data: { email, password } });
  if (!res.ok()) throw new Error(`${email} can neither sign up nor sign in: ${res.status()} ${await res.text()}`);
  const me = await answered<{ principal: { user: { id: string } } }>(context.get("auth/me"), 200, `who ${email} is`);
  return { context, id: me.principal.user.id };
}

async function tokenFor(who: Who, session: Session): Promise<ArmaturePerson> {
  const made = await answered<{ token: { secret: string } }>(session.context.post("tokens", { data: { name: "Stator" } }), 201, `a token for ${who}`);
  return { id: session.id, name: PEOPLE[who].name, email: PEOPLE[who].email, token: made.token.secret };
}

setup("seed the real Armature", async () => {
  const admin = await sessionOf("admin");
  const member = await sessionOf("member");
  const outsider = await sessionOf("outsider");
  const as = admin.context;

  const me = await answered<{ principal: { org: { slug: string } } }>(as.get("auth/me"), 200, "the installation's slug");

  // Nils keeps his account but loses the user grant every member joins with,
  // so his token works and shows him nothing.
  const { assignments } = await answered<{ assignments: Array<{ id: string; userId: string | null }> }>(as.get("role-assignments"), 200, "the grants");
  for (const grant of assignments.filter((each) => each.userId === outsider.id)) {
    await answered(as.delete(`role-assignments/${grant.id}`), 204, "take Nils's grant away");
  }

  if ((await as.get(`projects/${PROJECT.key}`)).status() === 404) {
    await answered(as.post("projects", { data: PROJECT }), 201, "the project");
  }
  const { teams } = await answered<{ teams: Array<{ id: string; name: string }> }>(as.get(`projects/${PROJECT.key}/teams`), 200, "the teams");
  const team =
    teams.find((each) => each.name === TEAM) ??
    (
      await answered<{ team: { id: string; name: string } }>(
        as.post(`projects/${PROJECT.key}/teams`, { data: { name: TEAM, description: null } }),
        201,
        "the team",
      )
    ).team;
  await as.post(`teams/${team.id}/members`, { data: { userId: member.id, lead: false } });

  const { issueTypes } = await answered<{ issueTypes: Array<{ id: string; name: string }> }>(as.get("issue-types"), 200, "the issue types");
  const epicType = issueTypes.find((each) => each.name === "Epic");
  if (!epicType) throw new Error(`Armature has no Epic type among ${issueTypes.map((each) => each.name).join(", ")}.`);

  const dates = datesThisMonth();
  const { issues: existing } = await answered<{ issues: SeededIssue[] }>(
    as.get("issues", { params: { q: `project = ${PROJECT.key} ORDER BY key`, limit: 100 } }),
    200,
    "the project's issues",
  );
  const file = async (summary: string, fields: Record<string, unknown>): Promise<SeededIssue> => {
    const found = existing.find((each) => each.summary === summary);
    if (found) return { key: found.key, summary };
    const made = await answered<{ issue: SeededIssue }>(as.post("issues", { data: { projectKey: PROJECT.key, summary, ...fields } }), 201, summary);
    return { key: made.issue.key, summary };
  };
  const epic = await file("Launch the wiki", { typeId: epicType.id });
  const guide = await file("Write the onboarding guide", {
    parentKey: epic.key,
    startDate: dates.guideStart,
    dueDate: dates.guideDue,
    teamId: team.id,
    assigneeId: member.id,
    priority: "high",
  });
  const runbooks = await file("Migrate the old runbooks", { parentKey: epic.key, startDate: dates.runbooksStart, dueDate: dates.runbooksDue, teamId: team.id });
  const signIn = await file("Fix the broken sign-in", { dueDate: dates.signInDue, priority: "highest" });

  // Mia uses an example theme of Armature's as her own.
  const { examples } = await answered<{ examples: Array<{ key: string; name: string; spec: unknown }> }>(
    member.context.get("themes/examples"),
    200,
    "the example themes",
  );
  const example = examples.find((each) => each.key === THEME_EXAMPLE) ?? examples[0];
  if (!example) throw new Error("Armature ships no example theme to start from.");
  const { themes } = await answered<{ themes: Array<{ id: string; name: string }> }>(member.context.get("themes"), 200, "Mia's themes");
  const theme =
    themes.find((each) => each.name === example.name) ??
    (
      await answered<{ theme: { id: string; name: string } }>(
        member.context.post("themes", { data: { name: example.name, spec: example.spec } }),
        201,
        "Mia's theme",
      )
    ).theme;
  await answered(member.context.put("themes/active", { data: { themeId: theme.id } }), 200, "Mia uses her theme");

  // The secret is shown once, so an endpoint an earlier run made goes first.
  const { webhooks } = await answered<{ webhooks: Array<{ id: string; name: string }> }>(as.get("webhooks"), 200, "the webhooks");
  for (const old of webhooks.filter((each) => each.name === WEBHOOK_NAME)) await as.delete(`webhooks/${old.id}`);
  const { webhook } = await answered<{ webhook: { secret: string } }>(
    as.post("webhooks", { data: { name: WEBHOOK_NAME, url: WEBHOOK_URL, topics: WEBHOOK_TOPICS } }),
    201,
    "the webhook to Stator",
  );

  const seed: Seed = {
    url: realArmatureURL(),
    orgSlug: me.principal.org.slug,
    project: PROJECT,
    team: { id: team.id, name: team.name },
    people: { admin: await tokenFor("admin", admin), member: await tokenFor("member", member), outsider: await tokenFor("outsider", outsider) },
    issues: { epic, guide, runbooks, signIn },
    dates,
    webhookSecret: webhook.secret,
    theme: { id: theme.id, name: theme.name },
  };
  writeFileSync(SEED_FILE, `${JSON.stringify(seed, null, 2)}\n`);
  for (const session of [admin, member, outsider]) await session.context.dispose();
});
