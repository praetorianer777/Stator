// Every spec shares the instance's one organization, with people and spaces of
// its own (fixtures/team.ts). These change what belongs to the organization as
// a whole, which every other spec would see, so they run one at a time after
// all the others, each file from the organization's settings as the bootstrap
// left them: its permissions, hub, brand, anonymous access and public links,
// themes, templates, sign-in provider, members, Armature connection, webhooks,
// audit log and example space. With them is the task report, whose choice of
// assignee offers only the first people of the organization by name, which
// every other spec's people would push the spec's own out of.
export const INSTANCE_SPECS = [
  "add-member",
  "anonymous-access",
  "armature",
  "armature-charts",
  "armature-roadmap",
  "armature-theme",
  "armature-webhook",
  "audit",
  "create-issues",
  "example-space",
  "export-brand",
  "gallery",
  "guests",
  "hub",
  "html-brand",
  "issue-block",
  "issue-list",
  "join",
  "login",
  "site-transfer",
  "page-links",
  "pdf-brand",
  "pdf-export",
  "permissions",
  "public-links",
  "sketches",
  "smart-links",
  "task-report",
  "team-calendar",
  "template-variables",
  "themes",
  "webhooks",
  "word-brand",
  "word-export",
];

/** What the metadata of a project of INSTANCE_SPECS says, so each file starts from the bootstrap's settings. */
export const RESETS_INSTANCE = "resetsInstance";
