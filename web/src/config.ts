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
