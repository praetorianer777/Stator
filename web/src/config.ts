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
