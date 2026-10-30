import { Link } from "@tanstack/react-router";
import { Page, PageHeader, Card } from "@/components/ui";
import { Icon } from "@/components/icons";
import { usePlaces } from "@/features/shell/places";
import { t } from "@/i18n";

/** /settings: the person's own pages, each with what it is for. The installation's are under Admin. */
export function SettingsHome() {
  const you = usePlaces().filter((place) => place.area === "you");
  return (
    <Page width="narrow" data-settings-home>
      <PageHeader title={t.settings.title} />
      <p className="mb-3 text-sm text-ink-muted">{t.settings.youIntro}</p>
      <Card className="divide-y divide-border" data-settings-card="you" aria-label={t.settings.you} role="region">
        {you.map((place) => {
          const Glyph = Icon[place.icon];
          return (
            <Link
              key={place.id}
              to={place.to}
              hash={place.hash}
              className="flex items-center gap-3 px-4 py-3 text-ink no-underline hover:bg-surface-raised"
              data-settings-link={place.id}
            >
              <Glyph className="shrink-0 text-ink-muted" />
              <span className="min-w-0 flex-1">
                <span className="block text-sm font-medium text-ink">{place.label}</span>
                <span className="block text-sm text-ink-muted">{place.detail}</span>
              </span>
              <Icon.ChevronRight className="shrink-0 text-ink-subtle" />
            </Link>
          );
        })}
      </Card>
    </Page>
  );
}
