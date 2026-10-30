/** The English strings of public reading, public links and sharing. */
export const publicReadingEn = {
  someone: "someone",
  included: "Shown from another page: open it",
  signIn: "Sign in",
  allSpaces: "All spaces",
  spaces: "Spaces",
  noSpaces: "Nothing is published here yet.",
  pages: "Pages in this space",
  children: "Pages in this folder",
  noChildren: "This folder holds no public pages.",
  updated: (date: string) => `Updated ${date}`,
  searchLabel: "Search the public pages",
  searchPlaceholder: "Search",
  search: "Search",
  searchTitle: (q: string) => `Results for ${q}`,
  noHits: "No public page matches those words. Try other words.",
  more: "More results",
  earlier: "Earlier results",
  notPublicTitle: "Not open to read without signing in",
  notPublicBody: "This is not public, or the address is wrong. If you belong to the installation, sign in to read it.",
  signInToRead: "Sign in to read it",
  failed: "This could not be loaded. Check your connection and try again.",
};

export const anonymousAccessEn = {
  title: "Reading without signing in",
  intro:
    "Let anybody on the internet read the spaces that allow it, without an account. They read published pages only, with nobody named in them: no authors, comments, reactions, readers or mentions. Restricted pages stay private.",
  enabled: "Let anybody read the spaces that allow it",
  indexable: "Let search engines list the public pages",
  address: (url: string) => `Public pages are at ${url}`,
  saved: "Saved.",
  spaceTitle: "Reading without signing in",
  spaceIntro: "Anybody may read this space's published pages without signing in. Restricted pages, drafts, comments and the names of people stay private.",
  spaceToggle: "Let anybody read this space without signing in",
  siteOff: "This takes effect once an administrator of the installation lets people read without signing in.",
  personal: "A personal space is never public. Move the pages to a team space to publish them.",
  spaceAddress: (url: string) => `Public address: ${url}`,
};

export const publicLinksEn = {
  title: "Public link",
  intro:
    "Anybody who holds a public link reads this page without an account, until the link is revoked or runs out. It opens this page alone: not the pages below it, its comments, its history or anybody's name.",
  newTitle: "Make a public link",
  label: "Label (optional)",
  labelPlaceholder: "Who it is for",
  expires: "Runs out",
  inDays: (n: number) => (n === 1 ? "In 1 day" : `In ${n} days`),
  never: "Never",
  create: "Make a public link",
  freshBody: "Copy the link now: it is shown this once. Anybody who has it can read this page.",
  address: "Public link",
  copy: "Copy link",
  copied: "Copied.",
  copyFailed: "The link could not be copied. Select it and copy it yourself.",
  listLabel: "Live public links",
  unlabelled: "Public link",
  you: "you",
  someoneGone: "somebody who has left",
  madeBy: (who: string, date: string) => `Made by ${who} on ${date}.`,
  runsOut: (date: string) => `Runs out on ${date}.`,
  neverRunsOut: "Works until it is revoked.",
  revoke: "Revoke",
  revokeNamed: (label: string) => `Revoke ${label}`,
  full: (n: number) => `This page has ${n} public links, as many as a page may have. Revoke one to make another.`,
  refusals: {
    cannotManage: "Only people who may edit this page make public links for it. Ask one of its editors.",
    off: "This installation does not allow public links. An administrator can allow them under Admin, Access.",
    personal: "A page of a personal space is never opened to anybody. Move it to a team space to make a link.",
    folder: "A folder only lists its pages. Make a link for a page inside it instead.",
    trashed: "This page is in the trash. Restore it to make a link.",
    unpublished: "This page, or a page above it, is not published yet. Publish it to make a link.",
    restricted:
      "This page, or a page above it, is restricted to some people, so it is opened to nobody else. Lift the restriction to make a link, or share the page with those people.",
  },
  goneTitle: "This link does not work any more",
  goneBody: "It was revoked, it ran out, or the page is no longer shared this way. Ask whoever sent it for a new one.",
  settingsTitle: "Public links",
  settingsIntro:
    "Let the editors of a page make links that anybody can read that one page through, without an account. Turned off, every link stops working until links are allowed again, and no new ones can be made.",
  settingsToggle: "Allow public links",
  saved: "Saved.",
};

export const shareEn = {
  button: "Share",
  title: (page: string) => `Share ${page}`,
  intro:
    "Send this page to people and groups, with a note if you like. They are told in the app and by email. Sharing gives nobody access: only people who may already view the page are told.",
  pickerLabel: "Send to",
  pickedLabel: "Sending to",
  nobodyYet: "Nobody picked yet.",
  full: (n: number) => `One share names at most ${n} people and groups. Share again for more.`,
  cannotView: "Cannot view this page",
  groupClosed: "Nobody in it can view this page",
  groupViewers: (viewers: number, members: number) =>
    viewers === members
      ? members === 1
        ? "1 member, who can view this page"
        : `${members} members, who can all view this page`
      : `${viewers} of ${members} members can view this page; only they are told`,
  closedNote:
    "Sharing does not give access. Remove the people and groups marked, or ask an administrator of the space, or somebody who may change the page's restrictions, to let them in first.",
  messageLabel: "Note (optional)",
  messageHint: (max: number) => `Up to ${max} characters, sent with the link to the page.`,
  viewersTitle: "Who can view this page",
  viewersEveryone: "Everyone in the installation can view this page.",
  viewersCount: (n: number) => (n === 1 ? "1 person can view this page:" : `${n} people can view this page:`),
  viewersRestricted: (n: number) => (n === 1 ? "This page is restricted. 1 person can view it:" : `This page is restricted. ${n} people can view it:`),
  viewersMore: (n: number) => `and ${n} more`,
  refusedClosed:
    "Nothing was shared, because the page is closed to somebody you picked. Remove the people and groups marked, or ask an administrator of the space, or somebody who may change the page's restrictions, to let them in first.",
  cancel: "Cancel",
  send: "Share",
  sent: (n: number) => (n === 1 ? "Shared with 1 person." : `Shared with ${n} people.`),
  done: "Done",
};
