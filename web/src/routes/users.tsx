import { createRoute } from "@tanstack/react-router";
import { Card, Page, PageHeader } from "@/components/ui";
import { AddMember } from "@/features/auth/AddMember";
import { JoinRequests } from "@/features/auth/JoinRequests";
import { UserList } from "@/features/users/UserList";
import { t } from "@/i18n";
import { adminRoute } from "./admin";

/** The installation's accounts: who waits to be let in, adding somebody with a password, and switching an account off or back on. */
export const usersRoute = createRoute({
  getParentRoute: () => adminRoute,
  path: "/users",
  component: function UsersPage() {
    return (
      <Page width="narrow" data-users-page="">
        <PageHeader title={t.admin.users.title} />
        <p className="mb-4 text-sm text-ink-muted">{t.admin.users.intro}</p>
        <div className="space-y-4">
          <JoinRequests />
          <Card className="p-4">
            <AddMember />
          </Card>
          <UserList />
        </div>
      </Page>
    );
  },
});
