/** The English strings, and the shape every other language has to fill. */
export const en = {
  common: {
    retry: "Retry",
    unassigned: "Unassigned",
  },
  nav: {
    everywhere: "Everywhere",
    whereYouAre: "Where you are",
    home: "Home",
    spaces: "Spaces",
    search: "Search",
    groupWiki: "Wiki",
    collapseSidebar: "Collapse the sidebar",
    expandSidebar: "Expand the sidebar",
    drawer: "Navigation",
    openDrawer: "Open the navigation",
    closeDrawer: "Close the navigation",
  },
  shell: {
    skipToContent: "Skip to content",
  },
  search: {
    placeholder: "Search pages and spaces",
    shortcut: "Ctrl K",
    comingTitle: "Search is on its way",
    comingBody: "Searching pages and spaces arrives with the first spaces. Until then, browse from the sidebar.",
  },
  theme: {
    system: "Auto",
    light: "Light",
    dark: "Dark",
    tooltip: (label: string) => `Theme: ${label}`,
  },
  account: {
    menu: "Your account",
    guest: "Guest",
    profile: "Your profile",
    signOut: "Sign out",
  },
  home: {
    title: "Home",
    emptyTitle: "Welcome to Stator",
    emptyBody: "Pages you write and visit will show up here. Start by opening the spaces your team shares.",
    emptyAction: "Browse spaces",
  },
  spaces: {
    title: "Spaces",
    emptyTitle: "No spaces yet",
    emptyBody: "A space holds a tree of pages for one team or topic. Spaces can be created once accounts arrive.",
  },
  notFound: {
    title: "Page not found",
    body: "The page you asked for does not exist. Check the address, or go back home.",
    action: "Go home",
  },
  routeError: {
    title: "This page could not be shown",
    retry: "Try again",
    fallback: "Something went wrong while loading this page. Try again, and reload if it keeps happening.",
  },
  api: {
    unexpected: (status: number) => `The server answered with status ${status}. Try again in a moment.`,
  },
};

export type Messages = typeof en;
