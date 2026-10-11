const PREFIX = "task-";

/** The fragment of an address that opens a page at one of its tasks. */
export function taskAnchor(taskId: string): string {
  return PREFIX + taskId;
}

export function isTaskAnchor(anchor: string): boolean {
  return anchor.startsWith(PREFIX) && anchor.length > PREFIX.length;
}
