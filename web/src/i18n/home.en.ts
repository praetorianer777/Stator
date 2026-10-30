/** The English strings of the home page and the example space. */
export const homeEn = {
  title: "Home",
  emptyTitle: "Welcome to Stator",
  emptyBody: "Pages you write and visit will show up here. Start by opening the spaces your team shares.",
  emptyAction: "Browse spaces",
  starredTitle: "Starred",
  starredEmpty: "Star a page or a space with the star in its header to keep it here.",
  recentTitle: "Recently viewed",
  recentEmpty: "The pages you open will show up here.",
  editedTitle: "Recently edited",
  editedEmpty: "The pages you publish or keep a draft of will show up here.",
  updatesTitle: "Updates",
  updatesAll: "All updates",
  updatesWatched: "Watched",
  updatesEmptyAll: "Nobody else has published a page you can read yet.",
  updatesEmptyWatched: "Nothing you watch has changed yet. Watch a page or a space from its header to follow it here.",
  published: (who: string, version: number) => (version === 1 ? `${who} created this page` : `${who} published version ${version}`),
  somebody: "Somebody",
  more: "Show more",
  draft: "Draft",
  unpublished: "Unpublished",
  spaceNamed: (name: string) => `Space ${name}`,
  unstar: (what: string) => `Unstar ${what}`,
  unstarred: (what: string) => `${what} is no longer starred.`,
  loadFailed: "This list could not be loaded. Try again in a moment.",
};

export const exampleSpaceEn = {
  title: "Example space",
  create: "Create the example space",
  creating: "Creating the example space",
  intro:
    "A space whose pages explain Stator to your team, with an example of every block a page can hold. It is written in the language you read Stator in now. Everybody in the installation may read it and comment on it.",
  exists: "The example space exists already:",
  archived: "It is archived. Take it out of the archive in its space settings to change it again.",
  underWay: "The example space is being made. This page opens it when it is ready.",
  slow: "The example space has not been begun yet. The worker that makes it may not be running; this page keeps checking, and if nothing changes, ask whoever runs Stator to start it.",
  gaveUp:
    "The example space has not been made after a quarter of an hour. Reload this page to check again, or ask whoever runs Stator whether its worker is running.",
  failures: {
    keys_taken: "The keys STATOR to STATOR9 are all taken by other spaces. Delete or rename one of them, then create the example space again.",
    forbidden: "Only administrators of the installation create the example space. Ask one of them to create it.",
    failed:
      "Creating the example space failed, and what was made of it was deleted. Create it again; if it fails once more, ask whoever runs Stator to look at the worker's log.",
    gone: "The example space was deleted while it was being made. Create it again.",
  },
  renewal: {
    available: "A newer example space is available. It brings:",
    intro: "Renewing it brings its guides up to date. Pages somebody changed are kept as they are, and the comments, stars and pages people added stay.",
    language: "Language of the pages",
    languages: { en: "English", de: "Deutsch" },
    renew: "Renew the example space",
    renewing: "Renewing the example space",
    underWay: "The example space is being renewed. This takes a few seconds.",
    done: "The example space is renewed.",
    added: "New pages",
    remade: "Made again, since they had been deleted",
    kept: "Kept as people changed them",
    retired: "No longer part of the example, left as they are",
    failures: {
      keys_taken: "Renewing the example space failed. Renew it again; if it fails once more, ask whoever runs Stator to look at the worker's log.",
      forbidden: "Only administrators of the installation renew the example space. Ask one of them to renew it.",
      failed:
        "Renewing the example space failed part way. The pages it renewed stay renewed and the others as they were; renew it again, and if it fails once more, ask whoever runs Stator to look at the worker's log.",
      gone: "The example space was deleted before it was renewed. Create the example space again to have the current one.",
    },
  },
};
