import { useContext, useState } from "react";
import { Node, mergeAttributes } from "@tiptap/core";
import { NodeViewWrapper, ReactNodeViewRenderer, type NodeViewProps } from "@tiptap/react";
import { useCalendarEvents } from "@/api/calendars";
import { Button } from "@/components/ui";
import { Icon } from "@/components/icons";
import { CALENDAR_NODE, calendarSettings, monthOf, monthSpan, type CalendarSettings } from "@/features/calendar/calendar";
import { CalendarDialog } from "@/features/calendar/CalendarDialog";
import { TeamCalendar } from "@/features/calendar/TeamCalendar";
import { t } from "@/i18n";
import { DocPageContext } from "./BlockViews";
import { settingsAttrs } from "./pageLists";

declare module "@tiptap/core" {
  interface Commands<ReturnType> {
    calendar: {
      /** Opens the settings dialog for a new calendar block; the dialog inserts it. */
      pickCalendar: () => ReturnType;
      insertCalendar: (settings: CalendarSettings) => ReturnType;
    };
  }
}

export interface CalendarOptions {
  /** Opens the settings dialog for a new block; without it the slash menu's entry does nothing. */
  pick: (() => void) | undefined;
}

// The calendar's own controls: ProseMirror would otherwise take a click on
// them as selecting the block.
const ownEvent = ({ event }: { event: Event }) => event.target instanceof Element && event.target.closest("a, button") !== null;

function CalendarView({ node, updateAttributes }: NodeViewProps) {
  const [editing, setEditing] = useState(false);
  const settings = calendarSettings(node.attrs);
  const page = useContext(DocPageContext);
  // The month the block opens on asks the same, so naming the calendar costs no second request.
  const span = monthSpan(monthOf(new Date()));
  const known = useCalendarEvents(settings.calendarId, span.from, span.to).data?.calendar;
  return (
    <NodeViewWrapper contentEditable={false} data-calendar-node="">
      <div className="doc-block">
        <div className="doc-block-settings" data-block-settings>
          <span className="min-w-0 flex-1 truncate text-xs">{known ? `${known.name} (${known.spaceKey})` : t.calendar.summary(settings.project)}</span>
          <Button size="sm" variant="secondary" icon={<Icon.Settings />} onClick={() => setEditing(true)} data-action="edit-calendar">
            {t.calendar.edit}
          </Button>
        </div>
        <TeamCalendar settings={settings} inEditor />
      </div>
      {editing && (
        <CalendarDialog
          initial={settings}
          spaceKey={known?.spaceKey ?? page?.spaceKey ?? null}
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

/** A month of a space's calendar beside an Armature project's due issues. The page stores which; each reader's view asks for them. */
export const CalendarNode = Node.create<CalendarOptions>({
  name: CALENDAR_NODE,
  group: "block",
  atom: true,
  selectable: true,
  draggable: true,
  addOptions() {
    return { pick: undefined };
  },
  addAttributes() {
    return settingsAttrs(calendarSettings);
  },
  parseHTML() {
    return [{ tag: "div[data-team-calendar-block]" }];
  },
  renderHTML({ HTMLAttributes }) {
    return ["div", mergeAttributes(HTMLAttributes, { "data-team-calendar-block": "" })];
  },
  // Its events are not the page's words, so copied text leaves them out.
  renderText() {
    return "";
  },
  addNodeView() {
    return ReactNodeViewRenderer(CalendarView, { stopEvent: ownEvent });
  },
  addCommands() {
    return {
      pickCalendar: () => () => {
        this.options.pick?.();
        return true;
      },
      insertCalendar:
        (settings) =>
        ({ commands }) => {
          const clean = calendarSettings({ ...settings });
          return clean.calendarId ? commands.insertContent({ type: this.name, attrs: clean }) : false;
        },
    };
  },
});
