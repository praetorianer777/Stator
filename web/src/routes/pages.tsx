import { createRoute, useNavigate } from "@tanstack/react-router";
import { Button, EmptyState, PageHeader } from "@/components/ui";
import { Icon } from "@/components/icons";
import { t } from "@/i18n";
import { appRoute } from "./app";

// Placeholders until the recent pages and search exist: each says what it is for, in the
// empty state, which is where a first-time visitor reads that.

function Home() {
  const navigate = useNavigate();
  return (
    <>
      <PageHeader title={t.home.title} />
      <EmptyState
        icon={<Icon.Home />}
        title={t.home.emptyTitle}
        description={t.home.emptyBody}
        action={
          <Button variant="secondary" onClick={() => navigate({ to: "/spaces" })}>
            {t.home.emptyAction}
          </Button>
        }
      />
    </>
  );
}

function Search() {
  return (
    <>
      <PageHeader title={t.nav.search} />
      <EmptyState icon={<Icon.Search />} title={t.search.comingTitle} description={t.search.comingBody} />
    </>
  );
}

export const homeRoute = createRoute({ getParentRoute: () => appRoute, path: "/", component: Home });
export const searchRoute = createRoute({ getParentRoute: () => appRoute, path: "/search", component: Search });
