import { useState } from "react";
import { Node, mergeAttributes } from "@tiptap/core";
import { NodeViewWrapper, ReactNodeViewRenderer, type NodeViewProps } from "@tiptap/react";
import { Button } from "@/components/ui";
import { Icon } from "@/components/icons";
import { IssueRoadmap } from "@/features/armature/IssueRoadmap";
import { IssueRoadmapDialog } from "@/features/armature/IssueRoadmapDialog";
import { ARMATURE_ROADMAP_NODE, roadmapSettings, type RoadmapSettings } from "@/features/armature/roadmap";
import { t } from "@/i18n";

declare module "@tiptap/core" {
  interface Commands<ReturnType> {
    armatureRoadmap: {
      /** Opens the settings dialog for a new roadmap; the dialog inserts it. */
      pickArmatureRoadmap: () => ReturnType;
      insertArmatureRoadmap: (settings: RoadmapSettings) => ReturnType;
    };
  }
}

export interface ArmatureRoadmapOptions {
  /** Opens the settings dialog for a new roadmap; without it the slash menu's entry does nothing. */
  pick: (() => void) | undefined;
}

// The roadmap's own controls: ProseMirror would otherwise take a click on them
// as selecting the block.
const ownEvent = ({ event }: { event: Event }) => event.target instanceof Element && event.target.closest("a, button") !== null;

function RoadmapView({ node, updateAttributes }: NodeViewProps) {
  const [editing, setEditing] = useState(false);
  const settings = roadmapSettings(node.attrs);
  return (
    <NodeViewWrapper contentEditable={false} data-armature-roadmap-node="">
      <div className="doc-block">
        <div className="doc-block-settings" data-block-settings>
          <code className="min-w-0 flex-1 truncate text-xs" title={settings.query}>
            {settings.project}: {settings.query}
          </code>
          <Button size="sm" variant="secondary" icon={<Icon.Settings />} onClick={() => setEditing(true)} data-action="edit-issue-roadmap">
            {t.armature.roadmap.edit}
          </Button>
        </div>
        <IssueRoadmap settings={settings} inEditor />
      </div>
      {editing && (
        <IssueRoadmapDialog
          initial={settings}
          isNew={false}
          onClose={() => setEditing(false)}
          onSave={(next) => {
            setEditing(false);
            updateAttributes(next);
          }}
        />
      )}
    </NodeViewWrapper>
  );
}

/**
 * The issues an NQL query matches in one project on a timeline. The page stores
 * what to draw; each reader's view asks Armature for the days.
 */
export const ArmatureRoadmap = Node.create<ArmatureRoadmapOptions>({
  name: ARMATURE_ROADMAP_NODE,
  group: "block",
  atom: true,
  selectable: true,
  draggable: true,
  addOptions() {
    return { pick: undefined };
  },
  addAttributes() {
    const read = (el: HTMLElement) =>
      roadmapSettings({
        project: el.getAttribute("data-project"),
        query: el.getAttribute("data-query"),
        groupBy: el.getAttribute("data-group-by"),
      });
    const fresh = roadmapSettings({});
    return {
      project: { default: fresh.project, parseHTML: (el) => read(el).project, renderHTML: (attrs) => ({ "data-project": attrs.project }) },
      query: { default: fresh.query, parseHTML: (el) => read(el).query, renderHTML: (attrs) => ({ "data-query": attrs.query }) },
      groupBy: { default: fresh.groupBy, parseHTML: (el) => read(el).groupBy, renderHTML: (attrs) => ({ "data-group-by": attrs.groupBy }) },
    };
  },
  parseHTML() {
    return [
      {
        tag: "div[data-armature-roadmap]",
        getAttrs: (el) => (roadmapSettings({ project: (el as HTMLElement).getAttribute("data-project") }).project ? null : false),
      },
    ];
  },
  renderHTML({ HTMLAttributes }) {
    return ["div", mergeAttributes(HTMLAttributes, { "data-armature-roadmap": "" })];
  },
  // Its rows are not the page's words, so copied text leaves it out.
  renderText() {
    return "";
  },
  addNodeView() {
    return ReactNodeViewRenderer(RoadmapView, { stopEvent: ownEvent });
  },
  addCommands() {
    return {
      pickArmatureRoadmap: () => () => {
        this.options.pick?.();
        return true;
      },
      insertArmatureRoadmap:
        (settings) =>
        ({ commands }) => {
          const clean = roadmapSettings({ ...settings });
          return clean.project && clean.query.trim() ? commands.insertContent({ type: this.name, attrs: clean }) : false;
        },
    };
  },
});
