import { useId } from "react";
import { useMe } from "@/api/auth";
import type { Comment } from "@/api/comments";
import type { Page } from "@/api/pages";
import { useCommentReaction, usePageReaction, type Reaction, type ReactionChange } from "@/api/reactions";
import { ErrorBanner, IconButton, Menu, Tooltip, cx } from "@/components/ui";
import { Icon } from "@/components/icons";
import { REACTION_CHOICES } from "@/config";
import { t } from "@/i18n";

/** Who put an emoji on, as the tooltip says it: the caller first, as you. */
export function whoReacted(reaction: Reaction, me: string | undefined): string {
  const others = reaction.people.filter((p) => p.id !== me).map((p) => p.name || t.reactions.formerMember);
  const names = reaction.mine ? [t.reactions.you, ...others] : others;
  return t.reactions.who(names, Math.max(0, reaction.count - names.length), reaction.emoji);
}

/**
 * The emoji on a page or comment, each a toggle for the caller's own, and a
 * picker to add another. Whoever may not react still reads who reacted.
 */
export function ReactionBar({
  reactions,
  label,
  canReact,
  busy,
  error,
  onChange,
  className,
}: {
  reactions: Reaction[];
  label: string;
  canReact: boolean;
  busy: boolean;
  error?: string;
  onChange: (change: ReactionChange) => void;
  className?: string;
}) {
  const me = useMe().data?.user.id;
  if (reactions.length === 0 && !canReact) return null;
  const used = new Set(reactions.filter((r) => r.mine).map((r) => r.emoji));
  return (
    <div className={className}>
      <ul aria-label={label} className="flex flex-wrap items-center gap-1.5" data-reactions="">
        {reactions.map((reaction) => (
          <li key={reaction.emoji}>
            <ReactionToggle reaction={reaction} who={whoReacted(reaction, me)} canReact={canReact} busy={busy} onChange={onChange} />
          </li>
        ))}
        {canReact && (
          <li>
            <Menu
              label={t.reactions.picker}
              items={REACTION_CHOICES.map((emoji) => ({
                icon: <span className="text-base">{emoji}</span>,
                label: t.reactions.names[emoji] ?? emoji,
                disabled: used.has(emoji),
                onSelect: () => onChange({ emoji, on: true }),
                attrs: { "data-reaction-choice": emoji },
              }))}
              trigger={(props) => (
                <IconButton
                  icon={<Icon.React />}
                  label={t.reactions.add}
                  size="sm"
                  variant="ghost"
                  onClick={props.toggle}
                  aria-haspopup={props["aria-haspopup"]}
                  aria-expanded={props["aria-expanded"]}
                  aria-controls={props["aria-controls"]}
                  className="rounded-full"
                  data-action="add-reaction"
                />
              )}
            />
          </li>
        )}
      </ul>
      {error && <ErrorBanner>{error}</ErrorBanner>}
    </div>
  );
}

function ReactionToggle({
  reaction,
  who,
  canReact,
  busy,
  onChange,
}: {
  reaction: Reaction;
  who: string;
  canReact: boolean;
  busy: boolean;
  onChange: (change: ReactionChange) => void;
}) {
  const whoId = useId();
  return (
    <Tooltip text={who} side="top">
      <button
        type="button"
        aria-pressed={reaction.mine}
        // aria-disabled keeps a reader who may not react able to focus it and hear who did.
        aria-disabled={!canReact || busy || undefined}
        aria-label={t.reactions.toggle(reaction.emoji, reaction.count)}
        aria-describedby={whoId}
        onClick={() => {
          if (canReact && !busy) onChange({ emoji: reaction.emoji, on: !reaction.mine });
        }}
        className={cx(
          "inline-flex h-7 items-center gap-1 rounded-full border px-2 text-sm transition-colors",
          "aria-disabled:cursor-default",
          reaction.mine ? "border-accent bg-accent-subtle text-ink" : "border-border bg-surface text-ink-muted",
          canReact && !reaction.mine && "hover:border-border-strong hover:bg-surface-raised hover:text-ink",
        )}
        data-reaction={reaction.emoji}
        data-mine={reaction.mine || undefined}
      >
        <span aria-hidden="true">{reaction.emoji}</span>
        <span aria-hidden="true" className="tabular-nums" data-reaction-count="">
          {reaction.count}
        </span>
        <span id={whoId} className="sr-only" data-reaction-who="">
          {who}
        </span>
      </button>
    </Tooltip>
  );
}

/** The emoji on a page itself, below its body; only published pages take them. */
export function PageReactions({ page }: { page: Page }) {
  const change = usePageReaction(page.id);
  if (page.unpublished) return null;
  return (
    <ReactionBar
      className="mt-8"
      reactions={page.reactions}
      label={t.reactions.page}
      canReact={page.can.comment}
      busy={change.isPending}
      error={change.error?.message}
      onChange={(next) => change.mutate(next)}
    />
  );
}

/** The emoji on one comment; a deleted comment shows none and takes none. */
export function CommentReactions({ pageId, comment, author, canReact }: { pageId: string; comment: Comment; author: string; canReact: boolean }) {
  const change = useCommentReaction(pageId, comment.id);
  if (comment.deleted) return null;
  return (
    <ReactionBar
      className="mt-1.5"
      reactions={comment.reactions}
      label={t.reactions.comment(author)}
      canReact={canReact}
      busy={change.isPending}
      error={change.error?.message}
      onChange={(next) => change.mutate(next)}
    />
  );
}
