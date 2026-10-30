/** The English strings of the shell: the rail, the sidebar, the account menu and the pages that are not there. */
export const navEn = {
  hub: "Hub",
  everywhere: "Everywhere",
  whereYouAre: "Where you are",
  home: "Home",
  spaces: "Spaces",
  search: "Search",
  tasks: "My tasks",
  reviews: "Reviews",
  notifications: "Notifications",
  groupWiki: "Wiki",
  collapseSidebar: "Collapse the sidebar",
  expandSidebar: "Expand the sidebar",
  drawer: "Navigation",
  openDrawer: "Open the navigation",
  buildVersion: (version: string) => `Stator ${version}`,
  buildCommit: (commit: string) => `Commit ${commit}`,
  buildCommitUnknown: "commit unknown",
  buildBuiltAt: (when: string) => `Built ${when}`,
  closeDrawer: "Close the navigation",
};

export const shellEn = {
  skipToContent: "Skip to content",
};

export const themeEn = {
  system: "Auto",
  light: "Light",
  dark: "Dark",
  tooltip: (label: string) => `Theme: ${label}`,
};

export const accountEn = {
  menu: "Your account",
  guest: "Guest",
  profile: "Your profile",
  themes: "Themes",
  tokens: "Access tokens",
  notifications: "Notification settings",
  watching: "Watching",
  signOut: "Sign out",
  switchTo: "Switch to",
  armatureAccount: "Armature account",
  exciterAccount: "Exciter account",
};

export const viaEn = {
  through: (name: string) => `through ${name}`,
  kinds: { token: "a personal access token", oauth: "a connected app" } as Record<"token" | "oauth", string>,
};

export const notFoundEn = {
  title: "Page not found",
  emptyTitle: "Nothing lives at this address",
  body: "The page you asked for does not exist. Check the address, or go back home.",
  action: "Go home",
};

export const routeErrorEn = {
  title: "This page could not be shown",
  emptyTitle: "Something went wrong",
  retry: "Try again",
  fallback: "Something went wrong while loading this page. Try again, and reload if it keeps happening.",
};

export const apiEn = {
  unexpected: (status: number) => `The server answered with status ${status}. Try again in a moment.`,
  unsaved: "That change could not be saved. Check your connection and try again.",
};
