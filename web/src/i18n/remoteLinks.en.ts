/** The English strings of links from other tools. */
export const remoteLinksEn = {
  title: "Linked from other tools",
  intro: "Each tool puts its own links here, with the token of somebody who may edit the page.",
  group: (source: string, count: number) => `${source} (${count})`,
  putBy: (name: string) => (name ? `Put by ${name}` : "Put by somebody no longer here"),
  remove: (title: string) => `Remove the link ${title}`,
  removeShort: "Remove",
  removed: (title: string) => `Removed the link ${title}. The tool that put it is told, so it leaves it off.`,
};
