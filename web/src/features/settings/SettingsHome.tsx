import { Link } from "@tanstack/react-router";
import { Page, PageHeader, Card } from "@/components/ui";
import { Icon } from "@/components/icons";
import { usePlaces, type Place } from "@/features/shell/places";
import { t } from "@/i18n";

/** /settings: a card of the person's own pages, and one of the organization's settings for site administrators. */
export function SettingsHome() {
  const places = usePlaces();
  const you = places.filter((place) => place.area === "you");
  const settings = places.filter((place) => place.area === "settings");
  return (
    <Page width="narrow" data-settings-home>
      <PageHeader title={t.settings.title} />
      <div className="grid gap-4 shell:grid-cols-2">
        <PlaceCard id="you" title={t.settings.you} intro={t.settings.youIntro} places={you} />
        {settings.length > 0 && <PlaceCard id="site" title={t.settings.site} intro={t.settings.siteIntro} places={settings} />}
      </div>
    </Page>
  );
}

function PlaceCard({ id, title, intro, places }: { id: string; title: string; intro: string; places: Place[] }) {
  return (
    <Card className="p-4" data-settings-card={id} aria-labelledby={`settings-card-${id}`} role="region">
      <h2 id={`settings-card-${id}`} className="text-base font-semibold text-ink">
        {title}
      </h2>
      <p className="mt-1 mb-3 text-sm text-ink-muted">{intro}</p>
      <ul className="flex flex-col gap-0.5">
        {places.map((place) => {
          const Glyph = Icon[place.icon];
          return (
            <li key={place.id}>
              <Link
                to={place.to}
                hash={place.hash}
                className="flex h-8 items-center gap-2 rounded-control px-2 text-sm text-ink no-underline hover:bg-surface-raised"
                data-settings-link={place.id}
              >
                <Glyph className="shrink-0 text-ink-muted" />
                <span className="min-w-0 flex-1 truncate">{place.label}</span>
              </Link>
            </li>
          );
        })}
      </ul>
    </Card>
  );
}
