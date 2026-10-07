import { useState } from "react";
import { Node, mergeAttributes } from "@tiptap/core";
import { NodeViewWrapper, ReactNodeViewRenderer, type NodeViewProps } from "@tiptap/react";
import { useTaskReport } from "@/api/tasks";
import { Button } from "@/components/ui";
import { Icon } from "@/components/icons";
import { TaskReport } from "@/features/taskReport/TaskReport";
import { TaskReportDialog } from "@/features/taskReport/TaskReportDialog";
import { TASK_REPORT_NODE, taskReportSettings, type TaskReportSettings } from "@/features/taskReport/report";
import { t } from "@/i18n";
import { ownEvent, settingsAttrs } from "./pageLists";

declare module "@tiptap/core" {
  interface Commands<ReturnType> {
    taskReport: {
      /** Opens the settings dialog for a new task report; the dialog inserts it. */
      pickTaskReport: () => ReturnType;
      insertTaskReport: (settings: TaskReportSettings) => ReturnType;
    };
  }
}

export interface TaskReportOptions {
  /** Opens the settings dialog for a new report; without it the slash menu's entry does nothing. */
  pick: (() => void) | undefined;
}

function TaskReportView({ node, updateAttributes }: NodeViewProps) {
  const [editing, setEditing] = useState(false);
  const settings = taskReportSettings(node.attrs);
  // The same query the report asks, so the name costs no second request.
  const name = useTaskReport(settings).data?.assigneeName ?? "";
  return (
    <NodeViewWrapper contentEditable={false} data-task-report-node="">
      <div className="doc-block">
        <div className="doc-block-settings" data-block-settings>
          <span className="min-w-0 flex-1 truncate text-xs">{t.taskReport.title(settings, name)}</span>
          <Button size="sm" variant="secondary" icon={<Icon.Settings />} onClick={() => setEditing(true)} data-action="edit-task-report">
            {t.taskReport.edit}
          </Button>
        </div>
        <TaskReport settings={settings} inEditor />
      </div>
      {editing && (
        <TaskReportDialog
          initial={settings}
          assigneeName={name}
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

/** The tasks of published pages a filter picks. The page stores the filter; each reader's view asks for the tasks they may read. */
export const TaskReportNode = Node.create<TaskReportOptions>({
  name: TASK_REPORT_NODE,
  group: "block",
  atom: true,
  selectable: true,
  draggable: true,
  addOptions() {
    return { pick: undefined };
  },
  addAttributes() {
    return settingsAttrs(taskReportSettings);
  },
  parseHTML() {
    return [{ tag: "div[data-task-report]" }];
  },
  renderHTML({ HTMLAttributes }) {
    return ["div", mergeAttributes(HTMLAttributes, { "data-task-report": "" })];
  },
  // Its tasks are other pages' words, so copied text leaves them out.
  renderText() {
    return "";
  },
  addNodeView() {
    return ReactNodeViewRenderer(TaskReportView, { stopEvent: ownEvent });
  },
  addCommands() {
    return {
      pickTaskReport: () => () => {
        this.options.pick?.();
        return true;
      },
      insertTaskReport:
        (settings) =>
        ({ commands }) =>
          commands.insertContent({ type: this.name, attrs: taskReportSettings({ ...settings }) }),
    };
  },
});
