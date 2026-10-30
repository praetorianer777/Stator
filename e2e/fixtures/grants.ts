import type { components } from "../../web/src/api/schema";
import { must, type StatorApi } from "./api";

type SpacePermission = components["schemas"]["CopyGrant"]["permissions"][number];

/** A subject as specs name it: a person, a group, or everyone, which is the Everyone group. */
export type TableSubject = { type: "everyone" } | { type: "user" | "group"; id: string };

/** One row of a space's table as the grids of before held it: a subject and what it may do there. */
export interface TableRow {
  subject: TableSubject;
  permissions: SpacePermission[];
}

const SPACE_WORDS: SpacePermission[] = ["view", "addPages", "addComments", "delete", "administer"];
const NAMES: Record<SpacePermission, string> = { view: "view", addPages: "add pages", addComments: "comment", delete: "delete", administer: "administer" };

/** What a set of space permissions amounts to: administer holds all, and any of them holds view. */
function closure(held: readonly string[]): SpacePermission[] {
  const all = held.includes("administer");
  const any = held.some((each) => each !== "use" && each !== "createSpace" && each !== "administerSite");
  return SPACE_WORDS.filter((each) => all || held.includes(each) || (each === "view" && any));
}

/** The id of the group that holds every member. */
export async function everyoneGroup(api: StatorApi): Promise<string> {
  const { groups } = must(await api.GET("/groups", { params: { query: { limit: 50 } } }));
  const everyone = groups.find((group) => group.builtin === "everyone");
  if (!everyone) throw new Error("The installation has no Everyone group.");
  return everyone.id;
}

/** The role that holds just these space permissions, made through the API when no role does yet. */
export async function roleFor(api: StatorApi, permissions: SpacePermission[]): Promise<string> {
  const wanted = closure(permissions);
  const { roles } = must(await api.GET("/roles"));
  const found =
    roles.filter((role) => role.builtin).find((role) => !role.orgWideOnly && closure(role.permissions).join() === wanted.join()) ??
    roles.find(
      (role) =>
        !role.orgWideOnly && closure(role.permissions).join() === wanted.join() && role.permissions.every((p) => SPACE_WORDS.includes(p as SpacePermission)),
    );
  if (found) return found.role;
  const words = wanted.map((each) => NAMES[each]);
  const name = words.length === 1 ? words[0]! : `${words.slice(0, -1).join(", ")} and ${words[words.length - 1]}`;
  return must(await api.POST("/roles", { body: { name: `E2e ${name}`, permissions: wanted } })).role.role;
}

/**
 * Makes a space's grants the table given, through the API as the caller: each
 * row is granted the role that holds just its permissions, then every other
 * grant in the space goes, the caller's own last.
 */
export async function setSpaceGrants(api: StatorApi, spaceKey: string, rows: TableRow[]): Promise<void> {
  const me = must(await api.GET("/auth/me")).user.id;
  const everyone = rows.some((row) => row.subject.type === "everyone") ? await everyoneGroup(api) : "";
  const { assignments } = must(await api.GET("/role-assignments", { params: { query: { space: spaceKey, limit: 200 } } }));
  const kept: string[] = [];
  for (const row of rows) {
    const role = await roleFor(api, row.permissions);
    const subject =
      row.subject.type === "everyone" ? { groupId: everyone } : row.subject.type === "group" ? { groupId: row.subject.id } : { userId: row.subject.id };
    const made = must(
      await api.POST("/role-assignments", { body: { role, spaceKey, userId: subject.userId ?? null, groupId: subject.groupId ?? null } }),
    ).assignment;
    kept.push(made.id);
  }
  const gone = assignments.filter((each) => !kept.includes(each.id));
  for (const each of [...gone.filter((g) => g.userId !== me), ...gone.filter((g) => g.userId === me)]) {
    const answer = await api.DELETE("/role-assignments/{assignmentID}", { params: { path: { assignmentID: each.id } } });
    if (!answer.response.ok) throw new Error(`Taking ${each.roleName} from ${each.userName ?? each.groupName} answered ${answer.response.status}.`);
  }
}
