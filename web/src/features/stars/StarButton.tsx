import type { Page } from "@/api/pages";
import type { Space } from "@/api/spaces";
import { useStarPage, useStarSpace } from "@/api/stars";
import { IconButton, cx } from "@/components/ui";
import { Icon } from "@/components/icons";
import { t } from "@/i18n";

/** The star glyph, filled while on, so the state shows without colour alone. */
export function StarGlyph({ on }: { on: boolean }) {
  return <Icon.Star className={cx(on && "fill-current text-accent")} />;
}

// A toggle keeps one name and says its state with aria-pressed, so a screen
// reader hears "Star Handbook, pressed" rather than a label that flips.
function Toggle({
  label,
  on,
  pending,
  size,
  onToggle,
  action,
}: {
  label: string;
  on: boolean;
  pending: boolean;
  size?: "sm" | "md";
  onToggle: () => void;
  action: string;
}) {
  return (
    <IconButton
      icon={<StarGlyph on={on} />}
      label={label}
      variant={size === "sm" ? "ghost" : "secondary"}
      size={size}
      aria-pressed={on}
      aria-busy={pending || undefined}
      // Not disabled while it saves: a disabled button would drop the keyboard's focus.
      onClick={() => {
        if (!pending) onToggle();
      }}
      data-action={action}
      data-starred={on || undefined}
    />
  );
}

/** Stars a space, from its directory row or its home page's header. */
export function SpaceStar({ space, size, onFailure }: { space: Space; size?: "sm" | "md"; onFailure?: (failed: boolean) => void }) {
  const star = useStarSpace(space.key);
  return (
    <Toggle
      label={t.star.starSpace(space.name)}
      on={space.starred}
      pending={star.isPending}
      size={size}
      action="star-space"
      onToggle={() => star.mutate(!space.starred, { onError: () => onFailure?.(true), onSuccess: () => onFailure?.(false) })}
    />
  );
}

/** The page header's star: a home page stands for its space, so it stars the space. */
export function PageStar({ page, space, onFailure }: { page: Page; space: Space; onFailure: (failed: boolean) => void }) {
  const star = useStarPage(page.id);
  if (page.home) return <SpaceStar space={space} onFailure={onFailure} />;
  return (
    <Toggle
      label={t.star.starPage(page.title)}
      on={page.starred}
      pending={star.isPending}
      action="star-page"
      onToggle={() => star.mutate(!page.starred, { onError: () => onFailure(true), onSuccess: () => onFailure(false) })}
    />
  );
}
