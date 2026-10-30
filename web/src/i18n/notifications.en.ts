/** The English strings of notifications. */
export const notificationsEn = {
  bell: "Notifications",
  bellUnread: (n: number) => (n === 1 ? "Notifications, 1 unread" : `Notifications, ${n} unread`),
  title: "Notifications",
  markAllRead: "Mark all as read",
  empty: "Nothing new. You hear here about pages you watch.",
  unread: "Unread",
  failed: "Your notifications could not be loaded. Try again in a moment.",
  settings: "Notification settings",
  sentence: (kind: string, actor: string, title: string, version: number | null, inComment = false) => {
    const who = actor || "Somebody";
    switch (kind) {
      case "assigned":
        return `${who} assigned you a task on ${title}`;
      case "due":
        return `A task of yours on ${title} is due`;
      case "review":
        return `${who} asks you to review a draft of ${title}`;
      case "reviewed":
        return `${who} decided on your draft of ${title}`;
      case "mentioned":
        return inComment ? `${who} mentioned you in a comment on ${title}` : `${who} mentioned you on ${title}`;
      case "shared":
        return `${who} shared ${title} with you`;
      case "replied":
        return `${who} replied in a thread on ${title}`;
      case "commented":
        return `${who} commented on ${title}`;
      case "resolved":
        return `${who} resolved or reopened a thread on ${title}`;
      case "published":
        return version ? `${who} published version ${version} of ${title}` : `${who} published ${title}`;
      case "created":
        return `${who} created ${title}`;
      case "posted":
        return `${who} posted ${title}`;
      case "expired":
        return `The verification of ${title} has run out. Check the page and verify it again.`;
      case "failed":
        return `Your scheduled publish of ${title} did not go out. Open the page to see why.`;
      default:
        return `${who} changed ${title}`;
    }
  },
  preferencesTitle: "Notifications",
  preferencesIntro:
    "Choose what you hear about, in the app and by email. An email is a copy of what the app shows, so a kind switched off in the app sends no email either.",
  kindColumn: "When",
  inApp: "In the app",
  email: "By email",
  kinds: {
    assigned: "Somebody assigns you a task",
    due: "A task assigned to you is due",
    review: "Somebody asks you to review a draft",
    reviewed: "A reviewer decides on your draft",
    mentioned: "Somebody mentions you",
    shared: "Somebody shares a page with you",
    replied: "Somebody replies in a thread you wrote in",
    commented: "Somebody comments on a page you watch",
    resolved: "A thread you wrote in is resolved or reopened",
    published: "A page you watch is published with a notice",
    created: "A page is first published below a page or space you watch",
    posted: "A post goes out in a blog or space you watch",
    expired: "The verification of a page you own runs out",
    failed: "A publish you scheduled cannot go out",
  } as Record<
    | "assigned"
    | "due"
    | "review"
    | "reviewed"
    | "mentioned"
    | "shared"
    | "replied"
    | "commented"
    | "resolved"
    | "published"
    | "created"
    | "posted"
    | "expired"
    | "failed",
    string
  >,
  inAppFor: (kind: string) => `In the app: ${kind}`,
  emailFor: (kind: string) => `By email: ${kind}`,
  digest: "When emails go out",
  digests: {
    off: "One email for each notification, at once",
    hourly: "One email an hour with everything new",
    daily: (hour: number) => `One email a day at ${String(hour).padStart(2, "0")}:00 UTC with everything new`,
  },
  digestHint: "A bundle leaves out what you have already read in the app.",
  autoWatch: "Watch the pages I create and publish",
  autoWatchHint: "Stopping watching a page keeps you from watching it again through your own edits.",
  save: "Save",
  saved: "Your notification settings are saved.",
};
