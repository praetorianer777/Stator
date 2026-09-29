import { administers, useMe } from "./auth";

/** Who is looking: their id, and whether they administer the organization. */
export interface Viewer {
  userId: string | undefined;
  administers: boolean;
}

/** The signed-in person, read from the session; nobody while it loads. */
export function useViewer(): Viewer {
  const { data } = useMe();
  return { userId: data?.user.id, administers: administers(data?.organization?.role) };
}
