import { useQueryClient } from "@tanstack/react-query";
import { useNavigate } from "@tanstack/react-router";
import { useLogout, useMe } from "@/api/auth";
import { accessQueryKey } from "@/api/permissions";
import { Button } from "@/components/ui";
import { Icon } from "@/components/icons";
import { APP_NAME } from "@/config";
import { t } from "@/i18n";

/**
 * What somebody signed in to an organization that has not let them use Stator
 * sees in place of everything: why, who can change it, and the way out.
 */
export function NoAccess() {
  const { data: me } = useMe();
  const queryClient = useQueryClient();
  const logout = useLogout();
  const navigate = useNavigate();
  const org = me?.organization?.name ?? t.permissions.thisOrganization;
  return (
    <div className="flex h-full flex-col bg-backdrop">
      <header className="flex h-12 shrink-0 items-center border-b border-border bg-surface px-4">
        <span className="text-sm font-semibold text-ink">{APP_NAME}</span>
      </header>
      <main id="main" tabIndex={-1} className="flex flex-1 items-start justify-center overflow-auto px-4 pt-[12vh] focus:outline-none" data-no-access>
        <div className="flex max-w-md flex-col items-center gap-3 rounded-overlay border border-border bg-surface px-6 py-10 text-center">
          <Icon.Lock size={24} className="text-ink-subtle" />
          <h1 className="text-lg font-semibold tracking-tight text-ink">{t.permissions.noAccessTitle(org)}</h1>
          <p className="text-sm text-ink-muted">{t.permissions.noAccessBody}</p>
          <div className="mt-2 flex flex-wrap justify-center gap-2">
            <Button
              variant="secondary"
              onClick={() => {
                // Everything else failed for the same reason, so everything is asked again.
                void queryClient.invalidateQueries();
                void queryClient.refetchQueries({ queryKey: accessQueryKey });
              }}
              data-action="retry-access"
            >
              {t.permissions.noAccessRetry}
            </Button>
            <Button variant="secondary" onClick={() => logout.mutate(undefined, { onSettled: () => navigate({ to: "/login" }) })} data-action="sign-out">
              {t.permissions.noAccessSignOut}
            </Button>
          </div>
        </div>
      </main>
    </div>
  );
}
