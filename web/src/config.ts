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
/** The longest title an expand block takes, matching the API's MaxExpandTitleLength. */
export const EXPAND_TITLE_MAX_LENGTH = 200;

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
/** The longest page title, matching the API's MaxTitleLength. */
export const PAGE_TITLE_MAX_LENGTH = 255;
/** Where a template's title takes the day the page is made, matching the API's template.DateToken. */
export const TEMPLATE_DATE_TOKEN = "{date}";
/** The picker's value for a page that starts empty; no template key can be empty. */
export const BLANK_TEMPLATE = "";
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

/** How far each level of the page tree is indented. */
export const TREE_INDENT_PX = 12;
/** The share of a tree row at its top and bottom where a dropped page goes beside it rather than under it. */
export const TREE_DROP_EDGE = 0.25;

/** How long the people and groups picker waits after a keystroke before it asks. */
export const PICKER_DEBOUNCE_MS = 200;
/** How many people and how many groups the picker offers at once; the API allows 50. */
export const PICKER_LIMIT = 8;
/** How long the editor waits after the last keystroke before it saves the draft. */
export const DRAFT_AUTOSAVE_MS = 1000;
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
