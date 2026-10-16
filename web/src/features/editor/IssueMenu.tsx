import { useEffect } from "react";
import type { ArmatureIssue } from "@/api/armature";
import { ARMATURE_FIND_MENU_WIDTH_PX, CARET_MENU_EDGE_PX } from "@/config";
import { FinderStatus, IssueOptions, useIssueFinder } from "@/features/armature/issueFinder";
import { t } from "@/i18n";
import { CARET_MENU_HEIGHT_CLASS, caretMenuPlaceNow } from "./caretMenu";
import { useFollowActive } from "./useFollowActive";

/**
 * The Armature issues what follows the trigger could mean, drawn under the
 * caret. It looks them up itself and hands them to the editor, whose keys
 * move through and pick them; the editor keeps focus throughout.
 */
export function IssueMenu({
  id,
  query,
  rect,
  active,
  onItems,
  onRemember,
  onPick,
  onHover,
}: {
  id: string;
  query: string;
  rect: DOMRect | null;
  active: number;
  /** Takes the issues to offer whenever they change. */
  onItems: (issues: ArmatureIssue[]) => void;
  /** Takes what keeps a picked issue among the person's recent ones. */
  onRemember: (remember: (key: string) => void) => void;
  onPick: (issue: ArmatureIssue) => void;
  onHover: (i: number) => void;
}) {
  const { state, issues, remember } = useIssueFinder(query);
  const follow = useFollowActive(active);

  useEffect(() => onItems(issues), [issues, onItems]);
  useEffect(() => onRemember(remember), [remember, onRemember]);
  // A menu opened next time starts from nothing, not from this one's issues.
  useEffect(() => () => onItems([]), [onItems]);

  if (!rect) return null;
  return (
    <div
      className={`fixed z-40 flex flex-col ${CARET_MENU_HEIGHT_CLASS} rounded-overlay border border-border bg-surface-overlay p-1 shadow-2`}
      style={{
        ...caretMenuPlaceNow(rect, ARMATURE_FIND_MENU_WIDTH_PX),
        width: `min(${ARMATURE_FIND_MENU_WIDTH_PX}px, calc(100vw - ${2 * CARET_MENU_EDGE_PX}px))`,
      }}
      data-issue-menu={state.kind}
    >
      <p role="status" className="px-2 py-1 text-xs text-ink-muted">
        <FinderStatus state={state} count={issues.length} />
      </p>
      {issues.length > 0 && (
        <IssueOptions
          id={id}
          label={t.armature.find.results}
          issues={issues}
          active={active}
          onPick={onPick}
          onHover={onHover}
          listRef={follow}
          className="min-h-0 overflow-y-hidden"
        />
      )}
    </div>
  );
}
