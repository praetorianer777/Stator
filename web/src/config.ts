/** Tunable values shared across the client. */

/** The product's name, as the title bar and the sidebar show it. */
export const APP_NAME = "Stator";

/** The release this client was built from, read from the root VERSION file at build time. */
export const APP_VERSION: string = __APP_VERSION__;

/** Where the API lives, relative to the page; the dev server proxies it. */
export const API_BASE = "/api/v1";

/** The sidebar, open and as the icon rail it becomes on narrow screens. */
export const SIDEBAR_WIDTH = 232;
export const SIDEBAR_RAIL_WIDTH = 48;

/** Below this viewport width the sidebar starts as a rail. */
export const SIDEBAR_RAIL_BELOW_PX = 1024;

/** Below this viewport width the rail goes and the sidebar is a drawer behind the top bar's menu button. */
// index.css names the same width as the `shell` breakpoint; a test keeps the two equal.
export const SIDEBAR_DRAWER_BELOW_PX = 768;

/** A skeleton waits this long before showing, so a fast load never flashes. */
export const SKELETON_DELAY_MS = 150;

/** A tooltip waits this long, so a pointer passing over shows nothing. */
export const TOOLTIP_DELAY_MS = 400;

/** The icon grid and stroke: every glyph is drawn to these two numbers. */
export const ICON_SIZE_PX = 16;
export const ICON_STROKE = 1.5;

/** The gap between a menu's trigger and its list, in pixels. */
export const MENU_GAP_PX = 4;

/** How long a query's answer stays fresh before a refetch. */
export const QUERY_STALE_MS = 10_000;

/** Where a visitor without a session is sent, with where they were headed in `next`. */
export const LOGIN_PATH = "/login";

/** Who is signed in changes only by signing in or out, so the answer is kept a minute. */
export const ME_STALE_MS = 60_000;

/** Where the organization last typed on the sign-in page is kept, so it is there next time. */
export const LAST_ORG_KEY = "stator.last-org";

/** Where this browser remembers the interface language the profile chose, so a reload starts in it. */
export const LANGUAGE_STORAGE_KEY = "stator.language";

/** The groups claim and scopes an identity provider starts with, matching the API's defaults. */
export const SSO_DEFAULT_GROUPS_CLAIM = "groups";
export const SSO_DEFAULT_SCOPES = "openid profile email";

/** The scope that lets a personal access token read and change nothing, as the API names it. */
export const TOKEN_READ_SCOPE = "read";
/** The lifetimes a new token is offered, in days; a token may also last until it is revoked. */
export const TOKEN_EXPIRY_DAYS = [7, 30, 90, 365] as const;
export const TOKEN_DEFAULT_EXPIRY_DAYS = 30;
/** The longest token name, matching the API's limit. */
export const TOKEN_NAME_MAX_LENGTH = 100;
/** Where an assistant reaches the MCP endpoint, and the name its client settings give Stator. */
export const MCP_PATH = `${API_BASE}/mcp`;
export const MCP_SERVER_NAME = "stator";
/** Milliseconds in a day, for a token's expiry. */
export const DAY_MS = 24 * 60 * 60 * 1000;

/** The largest file a theme takes, matching the API's limit. */
export const THEME_ASSET_MAX_BYTES = 12 * 1024 * 1024;
/** The most extra CSS a theme carries, matching the API's limit. */
export const THEME_CSS_MAX_BYTES = 32 * 1024;
/** How long a draft settles before the live preview is recompiled. */
export const THEME_PREVIEW_DEBOUNCE_MS = 150;
/** The size an icon is drawn at in the theme editor's list. */
export const THEME_ICON_PREVIEW_PX = 20;
/** Lines the extra CSS box shows before it scrolls. */
export const THEME_CSS_ROWS = 12;
/** The largest radius a theme may set, matching the API's limit. */
export const THEME_MAX_RADIUS = 32;
/** The furthest a cursor's point may sit from its picture's corner, matching the API's limit. */
export const THEME_MAX_HOTSPOT = 128;
/** The size a cursor picture is best drawn at; larger ones are refused by some browsers. */
export const THEME_CURSOR_PX = 32;
/** Bytes in a kilobyte, for the sizes the editor prints. */
export const KILOBYTE = 1024;

/** How many pieces a click throws, how long each lives, and how hard they fall. */
export const CONFETTI_PIECES_PER_CLICK = 28;
export const CONFETTI_LIFE_MS = 1100;
export const CONFETTI_GRAVITY_PX_PER_S2 = 1600;

/** How many wandering points the constellation backdrop draws per million pixels of page. */
export const CONSTELLATION_POINTS_PER_MEGAPIXEL = 70;
/** How close two of them have to be for a line to join them, in pixels. */
export const CONSTELLATION_LINK_PX = 150;
/** How fast a point wanders, in pixels per second, before its own variation. */
export const CONSTELLATION_SPEED_PX_PER_S = 14;

/** The heading levels the editor offers; a page's title sits above level 1. */
export const EDITOR_HEADING_LEVELS = [1, 2, 3] as const;
/** How far a heading's slug runs before it is cut, matching the API's MaxSlugLength. */
export const HEADING_SLUG_MAX_LENGTH = 64;
/** The shortest editing area, in lines, and the height of one. */
export const EDITOR_MIN_ROWS = 12;
export const EDITOR_LINE_HEIGHT_PX = 22;
/** The rows and columns a new table starts with, header row included. */
export const TABLE_DEFAULT_ROWS = 3;
export const TABLE_DEFAULT_COLS = 3;
/** How many people an at sign offers at once. */
export const MENTION_MAX_SUGGESTIONS = 8;
/** How long typing after an at sign settles before the people are looked up. */
export const MENTION_SEARCH_DEBOUNCE_MS = 150;
/** Find and replace in the editor, as aria-keyshortcuts names it; Cmd+F on a Mac. */
export const FIND_SHORTCUT = "Control+F";
/** How long "Link copied" stays after copying a heading's link. */
export const COPY_FEEDBACK_MS = 2000;
/** The deepest heading a new table of contents lists: all of them. */
export const TOC_DEFAULT_MAX_LEVEL = EDITOR_HEADING_LEVELS[EDITOR_HEADING_LEVELS.length - 1];
/** The most levels a child pages block may name, matching the API's MaxChildPagesDepth. */
export const CHILD_PAGES_MAX_DEPTH = 10;
/** How many pages one child pages block lists at most, matching the API's MaxBelow. */
export const CHILD_PAGES_LIMIT = 500;
/** How long the organization's hub stays fresh before it is asked for again; administrators change it rarely. */
export const HUB_STALE_MS = 60_000;
/** The longest title an expand block takes, matching the API's MaxExpandTitleLength. */
export const EXPAND_TITLE_MAX_LENGTH = 200;
/** The narrowest and widest share of its row a column takes, in percent, matching the API's MinColumnShare and MaxColumnShare. */
export const COLUMN_SHARE_MIN = 10;
export const COLUMN_SHARE_MAX = 80;
/**
 * The layouts a column section offers, by name and each column's share of the
 * row. The first of each count is the one inserting that many columns makes.
 */
export const COLUMN_LAYOUTS = [
  { key: "twoEven", widths: [50, 50] },
  { key: "twoWideLeft", widths: [67, 33] },
  { key: "twoWideRight", widths: [33, 67] },
  { key: "threeEven", widths: [33, 34, 33] },
  { key: "threeWideLeft", widths: [50, 25, 25] },
  { key: "threeWideMiddle", widths: [25, 50, 25] },
  { key: "threeWideRight", widths: [25, 25, 50] },
] as const;

/** The languages a code block offers, by highlighting grammar, and the name the picker shows. */
export const CODE_LANGUAGES = [
  { id: "bash", label: "Bash" },
  { id: "c", label: "C" },
  { id: "cpp", label: "C++" },
  { id: "csharp", label: "C#" },
  { id: "css", label: "CSS" },
  { id: "diff", label: "Diff" },
  { id: "go", label: "Go" },
  { id: "graphql", label: "GraphQL" },
  { id: "ini", label: "INI / TOML" },
  { id: "java", label: "Java" },
  { id: "javascript", label: "JavaScript" },
  { id: "json", label: "JSON" },
  { id: "kotlin", label: "Kotlin" },
  { id: "lua", label: "Lua" },
  { id: "makefile", label: "Makefile" },
  { id: "markdown", label: "Markdown" },
  { id: "php", label: "PHP" },
  { id: "python", label: "Python" },
  { id: "ruby", label: "Ruby" },
  { id: "rust", label: "Rust" },
  { id: "scss", label: "SCSS" },
  { id: "shell", label: "Shell session" },
  { id: "sql", label: "SQL" },
  { id: "swift", label: "Swift" },
  { id: "typescript", label: "TypeScript" },
  { id: "xml", label: "HTML / XML" },
  { id: "yaml", label: "YAML" },
] as const;

/** A space key's longest form, and the longest name and description, matching the API's limits. */
export const SPACE_KEY_MAX_LENGTH = 10;
export const SPACE_NAME_MAX_LENGTH = 100;
export const SPACE_DESCRIPTION_MAX_LENGTH = 1000;
/** The longest address a guest is invited by, as the mail standards and the API allow. */
export const GUEST_EMAIL_MAX_LENGTH = 254;
/** How many shortcuts a space holds, and the longest label and address, matching the API's limits. */
export const SHORTCUTS_MAX = 30;
export const SHORTCUT_LABEL_MAX_LENGTH = 100;
export const SHORTCUT_URL_MAX_LENGTH = 2000;
/** The schemes a shortcut's address may use; anything else could run a script. */
export const SHORTCUT_URL_SCHEMES: readonly string[] = ["http:", "https:"];
/** What an address somebody typed opens with: its own tab, no hold on this one, no word for it from us. */
export const EXTERNAL_LINK_REL = "noopener noreferrer nofollow";
/** A page's text measure, 44rem (616px at the app's 14px root): about 85 characters of Inter to a line. */
export const PAGE_MEASURE_REM = 44;
/** How wide a page's wide blocks grow, 96rem (1344px): past that a table's rows are too long to follow. */
export const PAGE_MAX_WIDTH_REM = 96;
/** The longest page title, matching the API's MaxTitleLength. */
export const PAGE_TITLE_MAX_LENGTH = 255;
/** Where a template's title takes the day the page is made, matching the API's template.DateToken. */
export const TEMPLATE_DATE_TOKEN = "{date}";
/** The picker's value for a page that starts empty; no template key can be empty. */
export const BLANK_TEMPLATE = "";
/** The API's bounds on one of the organization's templates, matching package template. */
export const TEMPLATE_NAME_MAX_LENGTH = 100;
export const TEMPLATE_DESCRIPTION_MAX_LENGTH = 500;
export const TEMPLATE_TITLE_MAX_LENGTH = 255;
export const TEMPLATE_VARIABLES_MAX = 20;
export const TEMPLATE_VARIABLE_LABEL_MAX_LENGTH = 80;
export const TEMPLATE_TEXT_VALUE_MAX_LENGTH = 500;
export const TEMPLATE_OPTIONS_MAX = 50;
/** The kinds a variable can be, in the order the editor offers them, as the API lists them. */
export const TEMPLATE_VARIABLE_KINDS = ["text", "date", "select", "person"] as const;
/** A date variable's default that stands for the day the page is made, as the API spells it. */
export const TEMPLATE_DEFAULT_TODAY = "today";
/** A variable's name as the API takes it; the form derives one from the label. */
export const TEMPLATE_VARIABLE_NAME_PATTERN = /^[a-z][a-z0-9_]{0,39}$/;
export const TEMPLATE_VARIABLE_NAME_MAX_LENGTH = 40;
/** Where administrators keep the organization's templates, and make or change one. */
export const TEMPLATES_PATH = "/settings/templates";
export const TEMPLATE_NEW_PATH = "/settings/templates/new";
export const TEMPLATE_EDIT_PATH = "/settings/templates/$templateKey";
/** The picker's value for a space that starts blank; the API takes no template then. */
export const BLANK_SPACE_TEMPLATE = "";
/** What everyone may do in a blank space, matching the database's space_default_grants. */
export const BLANK_SPACE_EVERYONE = ["view", "addPages", "addComments", "delete"] as const;
/** How far a page's title runs into its address before it is cut. */
export const PAGE_SLUG_MAX_LENGTH = 60;
/** The slug of a page whose title leaves nothing usable in an address. */
export const PAGE_SLUG_FALLBACK = "page";

/** The widths, in pixels, an image in a page can be set to; null keeps its natural size. */
export const IMAGE_WIDTHS = { small: 240, medium: 480, large: 720, original: null } as const;
/** The longest alternative text of an image, matching the API's limit. */
export const IMAGE_ALT_MAX_LENGTH = 500;
/** The widest an image's width attribute may be, matching the API's MaxImageWidth. */
export const IMAGE_MAX_WIDTH_PX = 4000;

/** How many pictures a gallery's row may hold on a wide screen, matching the API's bounds; a narrow screen shows two. */
export const GALLERY_COLUMNS = [2, 3, 4] as const;
/** The row a new gallery starts with. */
export const GALLERY_DEFAULT_COLUMNS = 3;
/** The most pictures one gallery holds, matching the API's MaxGalleryImages. */
export const GALLERY_MAX_IMAGES = 60;
/** The longest caption, which is the picture's alternative text too, matching the API's limit. */
export const GALLERY_CAPTION_MAX_LENGTH = IMAGE_ALT_MAX_LENGTH;

/** The lightbox's zoom: from the picture fitted to the screen up to this many times that. */
export const LIGHTBOX_MAX_ZOOM = 8;
/** How much one press of zoom in or out, or of + and -, multiplies the zoom by. */
export const LIGHTBOX_ZOOM_STEP = 1.5;
/** What a double click or double tap zooms an unzoomed picture to. */
export const LIGHTBOX_DOUBLE_ZOOM = 2.5;
/** How fast the wheel zooms: the zoom is multiplied by e to this times the pixels scrolled. */
export const LIGHTBOX_WHEEL_ZOOM_PER_PX = 0.0015;
/** How far one arrow key press moves a zoomed picture. */
export const LIGHTBOX_PAN_STEP_PX = 64;
/** How far a finger must sweep sideways across an unzoomed picture to go to the next or previous one. */
export const LIGHTBOX_SWIPE_PX = 48;

/** The colours a picture is annotated in: strong on a screenshot of either theme, and each edged in black or white besides. */
export const ANNOTATION_COLOURS = { red: "#e5191f", yellow: "#ffd60a", green: "#12a150", blue: "#1f6fe5", black: "#111111", white: "#ffffff" } as const;
/** The colour the annotation editor starts with. */
export const ANNOTATION_DEFAULT_COLOUR: keyof typeof ANNOTATION_COLOURS = "red";
/** How thick a box or an arrow is: this share of the picture's shorter side, and never thinner than the floor in picture pixels. */
export const ANNOTATION_STROKE_SHARE = 0.006;
export const ANNOTATION_STROKE_MIN_PX = 3;
/** How tall text is: this share of the picture's shorter side, and never smaller than the floor in picture pixels. */
export const ANNOTATION_TEXT_SHARE = 0.045;
export const ANNOTATION_TEXT_MIN_PX = 16;
/** The longest text one label holds. */
export const ANNOTATION_TEXT_MAX_LENGTH = 200;
/** How far a press must drag, in screen pixels, to draw a shape rather than click. */
export const ANNOTATION_MIN_DRAG_PX = 6;
/** How near, in screen pixels, a press must land to pick a shape's line. */
export const ANNOTATION_HIT_PX = 12;
/** How far one arrow key press moves or resizes a shape, as a share of the picture's shorter side. */
export const ANNOTATION_KEY_STEP_SHARE = 0.02;
/** The smallest crop, in picture pixels. */
export const ANNOTATION_MIN_CROP_PX = 8;
/** The most pixels a picture may have to be edited, what a phone's browser draws on one canvas. */
export const ANNOTATION_MAX_PIXELS = 16_777_216;
/** How much detail a JPEG or WebP keeps when the edited picture is saved. */
export const ANNOTATION_QUALITY = 0.92;
/** How many steps undo goes back. */
export const ANNOTATION_HISTORY_LIMIT = 100;
/** The pictures that can be annotated, with the name a person knows each type by, matching the API's attachment.Editable. */
export const ANNOTATION_TYPES: Readonly<Record<string, string>> = { "image/png": "PNG", "image/jpeg": "JPEG", "image/webp": "WebP" };

/** The videos the API shows in place, which a browser's own player plays. */
export const VIDEO_TYPES: readonly string[] = ["video/mp4", "video/webm", "video/ogg"];
/** The names a file chip offers to play by, since a chip does not carry the file's type. */
export const VIDEO_FILE_PATTERN = /\.(mp4|webm|ogv)$/i;

/** How far each level of the page tree is indented. */
export const TREE_INDENT_PX = 12;
/** The share of a tree row at its top and bottom where a dropped page goes beside it rather than under it. */
export const TREE_DROP_EDGE = 0.25;

/** How long the people and groups picker waits after a keystroke before it asks. */
export const PICKER_DEBOUNCE_MS = 200;
/** How many people and how many groups the picker offers at once; the API allows 50. */
export const PICKER_LIMIT = 8;
/** The longest note a share carries, matching the API's share.MaxMessageLength. */
export const SHARE_MESSAGE_MAX_LENGTH = 200;
/** How many people and groups one share names at most, matching the API's share.MaxRecipients. */
export const SHARE_MAX_RECIPIENTS = 20;
/** How many of the people who may view a page the share dialog names; it counts the rest. */
export const SHARE_VIEWERS_SHOWN = 8;
/** How long the editor waits after the last keystroke before it saves the draft. */
export const DRAFT_AUTOSAVE_MS = 1000;
/** How often a live page's reader asks for the page again, so what its editors type shows without a reload. */
export const LIVE_PAGE_REFRESH_MS = 3000;
/** How far ahead the publish dialog first offers a time to publish at, rounded up to the hour. */
export const SCHEDULE_DEFAULT_LEAD_MINUTES = 60;
/** How far ahead a publish may be scheduled at most, matching the API's page.MaxScheduleAhead. */
export const SCHEDULE_MAX_AHEAD_DAYS = 366;
/** How many minutes of a live page's saves one version of its history takes, matching the API's page.LiveVersionSpan. */
export const LIVE_VERSION_SPAN_MINUTES = 10;
/** How long the editor waits for a page's shared draft before it edits alone, saving to the person's own draft. */
export const COLLAB_CONNECT_TIMEOUT_MS = 6000;
/** The first and the longest wait before a lost connection to a shared draft is tried again; each try doubles it. */
export const COLLAB_RECONNECT_MIN_MS = 500;
export const COLLAB_RECONNECT_MAX_MS = 10_000;
/** How long a browser waits for somebody else to send a new shared draft's first content, past the API's page.SeedLease of 15 seconds, before it asks again. */
export const COLLAB_SEED_WAIT_MS = 20_000;
/** Where this browser keeps each shared draft's document in IndexedDB, by room, so changes made offline outlive a closed tab. */
export const COLLAB_LOCAL_PREFIX = "stator.collab.";
/** The colours people's carets and avatars take in a shared draft, as #rrggbb, which the caret needs; each person gets one by their id. */
export const COLLAB_COLORS: readonly string[] = ["#1d4ed8", "#b45309", "#047857", "#be185d", "#6d28d9", "#0e7490", "#b91c1c", "#4d7c0f"];
/** The initials on those colours, which a test holds to a readable contrast with each. */
export const COLLAB_INK = "#ffffff";
/** How many other people's avatars the editor shows before it counts the rest. */
export const COLLAB_AVATARS_SHOWN = 5;
/** The longest version comment, matching the API's page.MaxCommentLength. */
export const VERSION_COMMENT_MAX_LENGTH = 500;
/** How many versions the history shows at a time. */
export const HISTORY_PAGE_SIZE = 20;

/** How long quick search waits after the last key before it asks the server. */
export const QUICK_SEARCH_DEBOUNCE_MS = 150;
/** How many pages quick search and the recent pages list offer at once, within the API's limit of 20. */
export const QUICK_SEARCH_LIMIT = 8;
export const RECENT_PAGES_LIMIT = 8;

/** How many items each of the home page's lists reads at a time, within the API's limits of 100 and 50. */
export const HOME_STARS_PAGE_SIZE = 10;
export const HOME_UPDATES_PAGE_SIZE = 20;
export const HOME_EDITED_PAGE_SIZE = 8;
/** How many open tasks the home page shows, and the list of tasks reads at a time, within the API's limit of 100. */
export const HOME_TASKS_PAGE_SIZE = 5;
export const TASKS_PAGE_SIZE = 25;
/** How many hits one page of search results shows, within the API's limit of 100. */
export const SEARCH_PAGE_SIZE = 20;
/** The longest query, matching the API's limit. */
export const SEARCH_QUERY_MAX_LENGTH = 200;
/** How many people the author filter offers, the most the API gives at once. */
export const SEARCH_PEOPLE_LIMIT = 50;

/** The longest label, matching the API's label.MaxNameLength. */
export const LABEL_NAME_MAX_LENGTH = 40;
/** How many labels the label box offers at once, within the API's limit of 50. */
export const LABEL_SUGGESTION_LIMIT = 8;
/** How long the label box waits after the last key before it asks the server. */
export const LABEL_SUGGEST_DEBOUNCE_MS = 150;
/** How many pages one page of a label's list shows, within the API's limit of 100. */
export const LABEL_PAGE_SIZE = 20;

/** How often the notification badge asks for the unread count; it also asks when the window regains focus. */
export const UNREAD_POLL_MS = 30_000;
/** Above this many unread the badge shows the number with a plus. */
export const UNREAD_BADGE_MAX = 99;
/** How many notifications the panel lists, within the API's limit of 100. */
export const NOTIFICATION_PANEL_SIZE = 20;
/** How many watchers the watchers dialog lists, the most the API gives at once. */
export const WATCHERS_LIMIT = 100;
/** How many of the caller's watches one page of the watching list shows, within the API's limit of 100. */
export const WATCHES_PAGE_SIZE = 20;
/** The hour, in UTC, a daily digest goes out, matching the API's notify.DailyDigestHour. */
export const DAILY_DIGEST_HOUR_UTC = 8;

/** The lines a comment's editor starts with. */
export const COMMENT_EDITOR_ROWS = 4;
/** How long a thread a notification led to stays marked, so the eye finds it after the scroll. */
export const COMMENT_HIGHLIGHT_MS = 2500;
/** How often a new passage is marked afresh after somebody changed the page meanwhile, before giving up. */
export const INLINE_ANCHOR_RETRIES = 2;
/**
 * The emoji the reaction picker offers first, each named in t.reactions.names; any other comes
 * from the reaction search, and the API takes any one emoji.
 */
export const REACTION_CHOICES = ["👍", "❤️", "🎉", "😄", "😮", "👀", "🚀", "✅"] as const;
/** How many emoji the reaction search shows at once, one screen of the grid. */
export const REACTION_SEARCH_LIMIT = 32;

/** The key that, with Ctrl or Cmd and Alt, comments on the selected text; a KeyboardEvent code, since Alt changes the key on a Mac. */
export const INLINE_COMMENT_KEY_CODE = "KeyM";
/** The same shortcut as aria-keyshortcuts names it. */
export const INLINE_COMMENT_SHORTCUT = "Control+Alt+M";
/** How far below the selection the comment button floats, in pixels. */
export const INLINE_COMMENT_BUTTON_GAP_PX = 6;
/** The most characters of a passage a thread shows; the server keeps up to 500. */
export const INLINE_QUOTE_SHOWN_LENGTH = 200;

/** Where a person connects their Armature account, and the section's anchor there; the connect hints of issue chips link to it. */
export const PROFILE_PATH = "/settings/profile";
export const ARMATURE_SECTION_ID = "armature";
/** Where an administrator connects the organization's Armature. */
export const ARMATURE_SETTINGS_PATH = "/settings/armature";

/** Where an administrator reads the organization's audit log. */
export const AUDIT_PATH = "/settings/audit";
/** Entries the audit log shows per page, within the API's 200; the export has the rest. */
export const AUDIT_PAGE_SIZE = 50;
/** Where the audit log is downloaded as a spreadsheet, under the API's base. */
export const AUDIT_EXPORT_PATH = "/audit/export";
/** Where an administrator keeps the organization's outbound webhooks. */
export const WEBHOOKS_PATH = "/settings/webhooks";
/** The topic that takes every event, and the events a webhook may take, as the API lists them. */
export const WEBHOOK_TOPIC_ANY = "*";
export const WEBHOOK_TOPICS = ["page.published", "page.moved", "page.deleted", "comment.created"] as const;
/** The database's bounds on a webhook's name and address. */
export const WEBHOOK_NAME_MAX_LENGTH = 100;
export const WEBHOOK_URL_MAX_LENGTH = 2000;
/** Attempts the delivery log shows, within the API's 200. */
export const WEBHOOK_DELIVERIES_PAGE_SIZE = 50;
/** The header each delivery's signature travels in, for the receiver to check. */
export const WEBHOOK_SIGNATURE_HEADER = "X-Stator-Signature-256";
/** Where administrators read which pages nobody opened or published for a while. */
export const STALE_PATH = "/settings/stale";
/** Pages the stale report shows per page, within the API's 100. */
export const STALE_PAGE_SIZE = 25;
/** The periods the stale report offers, in days; it starts at the API's own default. */
export const STALE_AGE_DAYS = [30, 90, 180, 365, 730] as const;
export const STALE_DEFAULT_DAYS = 180;
/** What a link from the stale report carries, so reviewing a page is not a view that takes it off the report. */
export const STALE_REVIEW_FROM = "stale";
/** Readers a page's views dialog lists at a time, within the API's 100. */
export const PAGE_READERS_PAGE_SIZE = 25;
/** How every Armature personal access token and webhook secret starts, as Armature makes them. */
export const ARMATURE_TOKEN_PREFIX = "armature_pat_";
export const ARMATURE_WEBHOOK_SECRET_PREFIX = "armature_whs_";
/** The most issue keys one lookup asks for, as the API takes them; a page naming more asks in batches. */
export const ARMATURE_LOOKUP_MAX_KEYS = 50;
/** How long an issue chip's card waits before it shows, so a pointer passing over shows nothing. */
export const ARMATURE_CARD_DELAY_MS = 300;
/** How long the issue picker waits after a keystroke before it asks Armature for the key. */
export const ARMATURE_PICKER_DEBOUNCE_MS = 250;
/** The columns an Armature issue list may show, in the order the dialog offers them; the server's allowlist holds the same. */
export const ARMATURE_COLUMNS = ["key", "summary", "type", "status", "priority", "assignee", "reporter", "created", "updated", "due"] as const;
export type ArmatureColumn = (typeof ARMATURE_COLUMNS)[number];
/** What a new issue list shows, as armature.DefaultColumns. */
export const ARMATURE_DEFAULT_COLUMNS: readonly ArmatureColumn[] = ["key", "summary", "status", "assignee"];
/** The most columns and rows a list takes, and the rows a new one shows, as the API bounds them. */
export const ARMATURE_LIST_MAX_COLUMNS = 10;
export const ARMATURE_LIST_MAX_LIMIT = 100;
export const ARMATURE_LIST_DEFAULT_LIMIT = 20;
/** How many rows a list asks for at a time; Show more asks for the next ones, up to its limit. */
export const ARMATURE_LIST_PAGE_SIZE = 20;
/** The longest NQL query a list keeps, as armature.MaxQueryLength. */
export const ARMATURE_QUERY_MAX_LENGTH = 2000;
/** How long the list dialog waits after a keystroke before Armature checks the query. */
export const ARMATURE_QUERY_CHECK_DEBOUNCE_MS = 400;
/** Armature's priorities, lowest first, and its status categories in workflow order, for sorting rows. */
export const ARMATURE_PRIORITIES: readonly string[] = ["lowest", "low", "medium", "high", "highest"];
export const ARMATURE_STATUS_CATEGORIES: readonly string[] = ["todo", "in_progress", "done"];
/** The most issues one create files from a selection, as armature.MaxCreateItems. */
export const ARMATURE_CREATE_MAX_ITEMS = 50;
/** Armature's longest summary, as armature.MaxSummaryLength; a longer one is cut in the dialog. */
export const ARMATURE_SUMMARY_MAX_LENGTH = 255;
/** How often a page asks again while a link to Armature waits for the worker. */
export const ARMATURE_LINKS_POLL_MS = 2000;

/** The longest status label, matching the API's MaxStatusLength. */
export const STATUS_LABEL_MAX_LENGTH = 40;
/** The longest formula source, matching the API's MaxMathLength. */
export const MATH_MAX_LENGTH = 4000;
/** How many macro expansions one formula may take, so a macro that calls itself stops instead of holding the page. */
export const MATH_MAX_EXPAND = 1000;
/** The largest box, in em, a formula may ask for, so one cannot cover the page. */
export const MATH_MAX_SIZE = 20;
/** What a new formula holds until its author writes their own. */
export const MATH_DEFAULT_LATEX = "a^2 + b^2 = c^2";
/** How many lines the formula dialog's source field shows. */
export const MATH_SOURCE_ROWS = 5;
/** The longest diagram source, matching the API's MaxDiagramLength. */
export const DIAGRAM_MAX_LENGTH = 20000;
/** The most arrows one diagram may draw, so a huge graph cannot hold the page while it lays out. */
export const DIAGRAM_MAX_EDGES = 500;
/** How long the preview waits after the last keystroke before drawing the diagram again. */
export const DIAGRAM_PREVIEW_DELAY_MS = 300;
/** How many lines the diagram's source field shows. */
export const DIAGRAM_SOURCE_ROWS = 6;
/** What a new diagram holds until its author writes their own: a sketch that shows the syntax. */
export const DIAGRAM_DEFAULT_SOURCE = "flowchart LR\n  idea[Idea] --> draft[Draft] --> published[Published]";
/** The name a downloaded diagram is saved under. */
export const DIAGRAM_FILE_NAME = "diagram.svg";
/** How long a link's card is shown before it is asked for again; the server keeps it an hour. */
export const LINK_PREVIEW_STALE_MS = 5 * 60_000;
/** The views a link card offers, in the order its toolbar lists them. */
export const LINK_CARD_VIEWS = ["inline", "card", "embed"] as const;
/** The longest excerpt name, matching the API's MaxExcerptNameLength. */
export const EXCERPT_NAME_MAX_LENGTH = 80;
/** What a chart block shares issues out by, as armature.ChartGroupings. */
export const ARMATURE_CHART_GROUPINGS = ["status", "statusCategory", "type", "priority", "assignee"] as const;
export type ArmatureChartGrouping = (typeof ARMATURE_CHART_GROUPINGS)[number];
/** The windows a created against resolved chart offers, in days, within armature.MinChartDays to MaxChartDays. */
export const ARMATURE_CHART_DAY_CHOICES: readonly number[] = [7, 14, 30, 90, 180, 365];
/** A new created against resolved chart's window, as armature.DefaultChartDays. */
export const ARMATURE_CHART_DEFAULT_DAYS = 30;
/** The most slices a pie draws; the rest share one slice, Other, rather than take a colour nobody tells apart. */
export const ARMATURE_CHART_MAX_SLICES = 8;
/** A pie's size and its hole, in SVG units. */
export const ARMATURE_PIE_SIZE = 160;
export const ARMATURE_PIE_HOLE = 0.6;
/** A line chart's drawing box, in SVG units; it scales to the page's width. */
export const ARMATURE_FLOW_WIDTH = 640;
export const ARMATURE_FLOW_HEIGHT = 200;
/** How many evenly spaced ticks a line chart's axes carry at most. */
export const ARMATURE_FLOW_TICKS = 4;
/** The longest property name, matching the API's MaxPropertyKeyLength. */
export const PROPERTY_KEY_MAX_LENGTH = 60;
/** The most rows one properties block holds, matching the API's MaxProperties. */
export const PROPERTIES_MAX_ROWS = 50;
/** The most labels and columns a properties report asks for, matching MaxReportLabels and MaxReportColumns. */
export const PROPERTIES_REPORT_MAX_LABELS = 5;
export const PROPERTIES_REPORT_MAX_COLUMNS = 10;
/** How many pages a list block shows when it names no number, and the most, as document.DefaultListedPages and MaxListedPages. */
export const PAGE_LIST_DEFAULT_LIMIT = 10;
export const PAGE_LIST_MAX_LIMIT = 50;
/** The lengths a list block's settings offer, within 1 to PAGE_LIST_MAX_LIMIT. */
export const PAGE_LIST_LIMIT_CHOICES: readonly number[] = [5, 10, 20, 50];
/** How a content by label list matches its labels and orders its pages, as document.ListMatches and ListSorts. */
export const PAGE_LIST_MATCHES = ["all", "any"] as const;
export const PAGE_LIST_SORTS = ["updated", "title"] as const;
/** How many posts a latest blog posts block shows when it names no number; at most PAGE_LIST_MAX_LIMIT, as for any list block. */
export const BLOG_POSTS_DEFAULT_LIMIT = 5;
/** How many posts a blog shows at a time, as page.DefaultPostLimit. */
export const BLOG_PAGE_SIZE = 10;
/** The most opening characters of a post a block quotes; a blog shows all the API sends. */
export const BLOG_BLOCK_EXCERPT_LENGTH = 140;
/** How many tasks a task report shows when it names no number, and the most, as document.DefaultReportedTasks and MaxReportedTasks. */
export const TASK_REPORT_DEFAULT_LIMIT = 20;
export const TASK_REPORT_MAX_LIMIT = 100;
/** The lengths a task report's settings offer, within 1 to TASK_REPORT_MAX_LIMIT. */
export const TASK_REPORT_LIMIT_CHOICES: readonly number[] = [10, 20, 50, 100];
/** The due days and states a task report picks, as document.TaskReportDues and TaskReportStates. */
export const TASK_REPORT_DUES = ["any", "overdue", "today", "week", "none"] as const;
export const TASK_REPORT_STATES = ["open", "done", "all"] as const;
/** The bounds of a template button's words and its pattern for the page's title, as document.MaxButtonLabelLength and MaxButtonTitleLength. */
export const TEMPLATE_BUTTON_LABEL_MAX_LENGTH = 80;
export const TEMPLATE_BUTTON_TITLE_MAX_LENGTH = PAGE_TITLE_MAX_LENGTH;
/** What a contributors block counts, as document.ContributorScopes: the page alone, or it and the pages below it. */
export const CONTRIBUTOR_SCOPES = ["page", "tree"] as const;
/** How many people a contributors block names when it says no number, and the most, as document.DefaultContributors and MaxContributors. */
export const CONTRIBUTORS_DEFAULT_LIMIT = 10;
export const CONTRIBUTORS_MAX_LIMIT = 50;
/** The lengths a contributors block's settings offer, within 1 to CONTRIBUTORS_MAX_LIMIT. */
export const CONTRIBUTORS_LIMIT_CHOICES: readonly number[] = [5, 10, 20, 50];
/** The charts a table becomes, as document.TableCharts. */
export const TABLE_CHARTS = ["bar", "line", "pie"] as const;
/** The most series a chart from a table draws, one per categorical colour; more columns are left out. */
export const TABLE_CHART_MAX_SERIES = 8;
/** The plot of a bar or line chart from a table, before it takes its box's width. */
export const TABLE_CHART_WIDTH = 640;
export const TABLE_CHART_HEIGHT = 220;
/** How many gridlines a chart from a table aims for. */
export const TABLE_CHART_TICKS = 4;
/** What a bar's group leaves free on each side, as a share of its slot. */
export const TABLE_CHART_BAR_GAP = 0.2;
/** The kinds of a calendar's events, as calendar.Kinds. */
export const CALENDAR_EVENT_KINDS = ["event", "absence"] as const;
/** How long a calendar's name and an event's title may be, as calendar.MaxNameLength and MaxTitleLength. */
export const CALENDAR_NAME_MAX_LENGTH = 100;
export const CALENDAR_EVENT_TITLE_MAX_LENGTH = 200;
/** How many days one event may last, as calendar.MaxEventDays. */
export const CALENDAR_EVENT_MAX_DAYS = 366;
/** The weekday a calendar's weeks start on, 0 for Sunday: Monday, as ISO 8601 counts weeks. */
export const CALENDAR_WEEK_START = 1;
/** The hour a new event with times starts at, and how many minutes it lasts. */
export const CALENDAR_DEFAULT_START_HOUR = 9;
export const CALENDAR_DEFAULT_EVENT_MINUTES = 60;
/** What a roadmap block puts its rows under, as armature.RoadmapGroupings. */
export const ARMATURE_ROADMAP_GROUPINGS = ["epic", "team"] as const;
export type ArmatureRoadmapGrouping = (typeof ARMATURE_ROADMAP_GROUPINGS)[number];
/** Past this many days a roadmap's axis marks months; within it, weeks. */
export const ARMATURE_ROADMAP_WEEKLY_DAYS = 70;
/** The least room, in pixels, between two labels of a roadmap's axis; closer ones skip a label. */
export const ARMATURE_ROADMAP_LABEL_GAP_PX = 64;
/** The room, in pixels, a label of a roadmap's axis needs to its right; nearer the edge it is left out. */
export const ARMATURE_ROADMAP_LABEL_ROOM_PX = 48;
/** Days left either side of a roadmap's earliest and latest day, so no bar touches its edge. */
export const ARMATURE_ROADMAP_MARGIN_DAYS = 3;
/** How many emoji the appearance dialog offers for a page at once. */
export const PAGE_ICON_CHOICES = 24;
/** Where a new cover's focus starts, in percent: its middle. */
export const COVER_FOCUS_DEFAULT = 50;
/** How far one arrow key moves a cover's focus, in percent. */
export const COVER_FOCUS_STEP = 5;
/** How many emoji a colon offers at once. */
export const EMOJI_MAX_SUGGESTIONS = 8;
/** What a colon offers before a letter is typed, by shortcode, most used first. */
export const EMOJI_COMMON: readonly string[] = ["+1", "white_check_mark", "x", "warning", "tada", "rocket", "eyes", "heart"];

/** What one Markdown import may weigh, its files included, matching the API's limit. */
export const MARKDOWN_IMPORT_MAX_BYTES = 100 * 1024 * 1024;

/** The file names a Markdown import reads as pages; anything else is a file a page may show. */
export const MARKDOWN_FILE_PATTERN = /\.(md|markdown)$/i;
/** An archive the API unpacks before it reads the pages in it. */
export const ZIP_FILE_PATTERN = /\.zip$/i;

/** The terms a page's verification is offered for, in days; the API allows 1 to 730. */
export const VERIFY_TERM_DAYS = [30, 90, 180, 365] as const;
/** The term offered first, as the API's own default. */
export const VERIFY_DEFAULT_DAYS = 90;

/** Where the pages anybody may read without signing in live, followed by the organization's slug. */
export const PUBLIC_PATH = "/public";
/** A public answer is the same for everybody and may be a minute old, as the API lets caches keep it. */
export const PUBLIC_STALE_MS = 60_000;
/** Hits a public search shows at a time. */
export const PUBLIC_SEARCH_LIMIT = 20;
/** The part of a public address a link's token follows, after the organization, as the API makes it. */
export const PUBLIC_LINK_SEGMENT = "link";
/** The lifetimes a public link is offered, in days; it may also work until it is revoked. */
export const PUBLIC_LINK_EXPIRY_DAYS = [1, 7, 30, 90] as const;
/** The lifetime offered first: a link nobody remembers stops by itself. */
export const PUBLIC_LINK_DEFAULT_EXPIRY_DAYS = 30;
/** The longest label a public link takes, matching the API's limit. */
export const PUBLIC_LINK_LABEL_MAX_LENGTH = 60;

/** How long a print view stays quiet, nothing loading or drawing, before it says it is ready to print. */
export const PRINT_SETTLE_MS = 300;
/** What a print view waits out: a skeleton, a busy region, a diagram being drawn or a preview loading. */
export const PRINT_PENDING_SELECTOR = '[data-skeleton], [aria-busy="true"], [data-diagram-state="drawing"], [data-preview-loading]';

/** How often a page that asked for the example space asks how its making goes. */
export const EXAMPLE_SPACE_POLL_MS = 1000;
/** After this long without the worker beginning the example space, the page says it may not be running. */
export const EXAMPLE_SPACE_SLOW_MS = 60_000;
/** After this long, past the API's limit on one making, the page stops asking and says what to do. */
export const EXAMPLE_SPACE_GIVE_UP_MS = 15 * 60_000;
