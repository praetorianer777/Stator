import { administers, useMe } from "@/api/auth";
import { useAccess } from "@/api/permissions";

// The session's word stands in while the installation's answer loads, so an
// administrator's menu does not flicker on every visit. Both come from grants.

/** Whether the caller administers the installation. */
export function useCanAdministerSite(): boolean {
  const { data: access } = useAccess();
  const { data: me } = useMe();
  return access?.administer ?? administers(me);
}

/** Whether the caller may make spaces; the administrator's role holds it as it is installed. */
export function useCanCreateSpace(): boolean {
  const { data: access } = useAccess();
  const { data: me } = useMe();
  return access?.createSpace ?? administers(me);
}
