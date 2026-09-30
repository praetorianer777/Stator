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
/** How long "Link copied" stays after copying a heading's link. */
export const COPY_FEEDBACK_MS = 2000;

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

/** How long the editor waits after the last keystroke before it saves the draft. */
export const DRAFT_AUTOSAVE_MS = 1000;
/** The longest version comment, matching the API's page.MaxCommentLength. */
export const VERSION_COMMENT_MAX_LENGTH = 500;
/** How many versions the history shows at a time. */
export const HISTORY_PAGE_SIZE = 20;
