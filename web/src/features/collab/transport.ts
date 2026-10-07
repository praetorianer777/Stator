/**
 * Whether the editor tries to edit together at all, and how it opens a
 * socket. The unit tests turn it off, so every editor there edits alone
 * unless a test hands it a socket of its own.
 */
export const collabTransport: { enabled: boolean; socket: (url: string) => WebSocket } = {
  enabled: typeof WebSocket !== "undefined",
  socket: (url) => new WebSocket(url),
};
