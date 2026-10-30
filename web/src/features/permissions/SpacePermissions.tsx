import { permissionWords, useRoles } from "@/api/access";
import type { Space } from "@/api/spaces";
import { useToast } from "@/components/ui";
import { RoleTable } from "@/features/access/RoleTable";
import { t } from "@/i18n";
import { CopyPermissions } from "./CopyPermissions";

/**
 * Who holds which role in a space: grant one to a person or a group, take one
 * away. What a role holds is the installation's matrix; here it is said in words.
 */
export function SpacePermissions({ space }: { space: Space }) {
  const toast = useToast();
  if (!space.can.administer) return <p className="text-sm text-ink-muted">{t.permissions.notSpaceAdmin}</p>;
  return (
    <div className="space-y-4" data-space-permissions>
      <p className="text-sm text-ink-muted">{t.permissions.spaceIntro}</p>
      <RoleTable spaceKey={space.key} />
      <RolesInWords />
      {!space.owner && <CopyPermissions space={space} blocked={false} onCopied={(from) => toast.success(t.permissionCopy.copied(from))} />}
    </div>
  );
}

const INSTALLATION_ONLY = ["use", "createSpace", "administerSite"];

/** What each role a space grants holds, so the person granting knows what they give. */
function RolesInWords() {
  const { data: roles } = useRoles();
  const spaceRoles = (roles ?? []).filter((role) => !role.orgWideOnly);
  if (spaceRoles.length === 0) return null;
  return (
    <section className="space-y-2" data-space-roles>
      <h2 className="text-sm font-semibold text-ink">{t.permissions.rolesTitle}</h2>
      <dl className="grid gap-x-4 gap-y-1 text-sm sm:grid-cols-[auto_1fr]">
        {spaceRoles.map((role) => (
          <div key={role.role} className="contents" data-space-role={role.role}>
            <dt className="font-medium text-ink">{role.name}</dt>
            <dd className="text-ink-muted">
              {role.permissions
                .filter((permission) => !INSTALLATION_ONLY.includes(permission))
                .map((permission) => permissionWords(permission))
                .join(", ")}
            </dd>
          </div>
        ))}
      </dl>
      <p className="text-xs text-ink-subtle">{t.permissions.rolesHint}</p>
    </section>
  );
}
