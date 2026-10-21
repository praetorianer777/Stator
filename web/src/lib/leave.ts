/** Sends the browser to another site, as a connected app's answer goes back to it; a module so a test can stand in. */
export function leave(to: string) {
  window.location.assign(to);
}
