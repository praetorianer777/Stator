import { administers, useMe } from "@/api/auth";
import { useAccess } from "@/api/permissions";

// The role in the session stands in while the organization's answer loads, so
// an administrator's menu does not flicker on every visit.

/** Whether the caller administers the organization. */
export function useCanAdministerOrg(): boolean {
  const { data: access } = useAccess();
  const { data: me } = useMe();
  return access?.administer ?? administers(me?.organization?.role);
}

/** Whether the caller may make spaces. */
export function useCanCreateSpace(): boolean {
  const { data: access } = useAccess();
  const { data: me } = useMe();
  return access?.createSpace ?? administers(me?.organization?.role);
}
