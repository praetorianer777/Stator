import { createRoute } from "@tanstack/react-router";
import { useMe } from "@/api/auth";
import { Page, PageHeader } from "@/components/ui";
import { BrandSettings } from "@/features/brand/BrandSettings";
import { installationName } from "@/features/shell/places";
import { t } from "@/i18n";
import { adminRoute } from "./admin";

/** The installation itself: what it is called, and the brand its exports carry. */
export const installationRoute = createRoute({
  getParentRoute: () => adminRoute,
  path: "/installation",
  component: function InstallationPage() {
    const { data: me } = useMe();
    return (
      <Page width="narrow" className="space-y-8" data-installation-page="">
        <PageHeader title={t.admin.installation.title} />
        <section aria-labelledby="installation-name" className="space-y-1">
          <h2 id="installation-name" className="text-base font-semibold text-ink">
            {t.admin.installation.name}
          </h2>
          <p className="text-sm text-ink" data-installation-name-shown>
            {installationName(me)}
          </p>
          <p className="text-sm text-ink-muted">{t.admin.installation.nameHint}</p>
        </section>
        <BrandSettings />
      </Page>
    );
  },
});
