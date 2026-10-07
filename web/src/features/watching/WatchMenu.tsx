import { useState } from "react";
import type { Page } from "@/api/pages";
import type { Space } from "@/api/spaces";
import { useUnwatchPage, useWatchPage, useWatchSpace } from "@/api/watching";
import { Button, Menu, type MenuItem } from "@/components/ui";
import { Icon } from "@/components/icons";
import { t } from "@/i18n";
import { WatchersDialog } from "./WatchersDialog";

/** What covers a page from above, in words, or nothing when no watch above does. */
export function coveredBy(page: Page): string | undefined {
  const above = page.watching.inherited;
  if (!above) return undefined;
  return above.kind === "space" || !above.page ? t.watch.coveredBySpace : t.watch.coveredBySubtree(above.page.title);
}

function marked(label: string, on: boolean) {
  return on ? (
    <>
      <span className="flex-1">{label}</span>
      <Icon.Check />
      <span className="sr-only"> {t.watch.selected}</span>
    </>
  ) : (
    label
  );
}

/** The page header's watch button: this page, it and the pages below, or the whole space, and who else watches. */
export function WatchMenu({ page, space, onFailure }: { page: Page; space: Space; onFailure: (failed: boolean) => void }) {
  const watchPage = useWatchPage(page.id);
  const unwatchPage = useUnwatchPage(page.id);
  const watchSpace = useWatchSpace(space.key);
  const [showWatchers, setShowWatchers] = useState(false);
  const own = page.watching.page || page.watching.subtree;
  const covered = coveredBy(page);
  const watching = own || Boolean(covered) || space.watching;
  const told = { onError: () => onFailure(true), onSuccess: () => onFailure(false) };

  const items: MenuItem[] = [
    { label: marked(t.watch.page, page.watching.page), onSelect: () => watchPage.mutate(false, told), attrs: { "data-action": "watch-page" } },
  ];
  // A post has no pages below it to watch.
  if (page.kind !== "post") {
    items.push({
      label: marked(t.watch.subtree, page.watching.subtree),
      onSelect: () => watchPage.mutate(true, told),
      attrs: { "data-action": "watch-subtree" },
    });
  }
  if (own) items.push({ label: t.watch.stopPage, onSelect: () => unwatchPage.mutate(undefined, told), attrs: { "data-action": "unwatch-page" } });
  items.push(
    space.watching
      ? { label: t.watch.stopSpace, onSelect: () => watchSpace.mutate(false, told), attrs: { "data-action": "unwatch-space" } }
      : { label: t.watch.space, onSelect: () => watchSpace.mutate(true, told), attrs: { "data-action": "watch-space" } },
  );
  // A disabled item stays in the arrow order, so the reader hears what covers the page.
  if (covered && !own) items.push({ label: covered, onSelect: () => {}, disabled: true, attrs: { "data-watch-covered": "" } });
  items.push({ label: t.watch.watchers, icon: <Icon.Users />, onSelect: () => setShowWatchers(true), attrs: { "data-action": "show-watchers" } });

  return (
    <>
      <Menu
        label={t.watch.menu(page.title)}
        align="end"
        items={items}
        trigger={(props) => (
          <Button
            variant="secondary"
            icon={<Icon.Eye />}
            onClick={props.toggle}
            aria-haspopup={props["aria-haspopup"]}
            aria-expanded={props["aria-expanded"]}
            aria-controls={props["aria-controls"]}
            loading={watchPage.isPending || unwatchPage.isPending || watchSpace.isPending}
            data-action="watch-menu"
            data-watching={watching || undefined}
          >
            {watching ? t.watch.watching : t.watch.button}
          </Button>
        )}
      />
      {showWatchers && <WatchersDialog page={page} onClose={() => setShowWatchers(false)} />}
    </>
  );
}
