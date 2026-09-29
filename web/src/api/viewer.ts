/** Who is looking: their id, and whether they administer the organization. */
export interface Viewer {
  userId: string | undefined;
  administers: boolean;
}

/**
 * The signed-in person. There is no sign-in until issue #5, which answers this
 * from the session; until then nobody is anybody and nobody administers.
 */
export function useViewer(): Viewer {
  return { userId: undefined, administers: false };
}
