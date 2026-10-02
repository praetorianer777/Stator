import { useInfiniteQuery, useMutation, useQueryClient } from "@tanstack/react-query";
import { HOME_TASKS_PAGE_SIZE, TASKS_PAGE_SIZE } from "@/config";
import { api } from "./client";
import { pageQueryKey } from "./pages";
import type { components } from "./schema";

type Wire = components["schemas"];

/** A checklist item of a published page, as the list of the caller's tasks holds it. */
export type Task = Wire["Task"];
/** Which of the caller's tasks to list. */
export type TaskState = "open" | "done";

export const tasksQueryKey = ["tasks"] as const;

/** The caller's tasks of one state, a window at a time: open ones soonest due first, done ones latest first. */
export function useMyTasks(state: TaskState, pageSize: number = TASKS_PAGE_SIZE) {
  return useInfiniteQuery({
    queryKey: [...tasksQueryKey, state, pageSize],
    initialPageParam: "",
    queryFn: async ({ pageParam }) => (await api.GET("/tasks", { params: { query: { state, limit: pageSize, cursor: pageParam || undefined } } })).data!,
    getNextPageParam: (last) => last.next ?? undefined,
  });
}

/** The few open tasks the home page shows beside its other lists. */
export function useHomeTasks() {
  return useMyTasks("open", HOME_TASKS_PAGE_SIZE);
}

/** Ticks a task off or opens it again, which publishes its page; both the page and the lists read it afresh. */
export function useSetTaskDone() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: async ({ pageId, taskId, done }: { pageId: string; taskId: string; done: boolean }) =>
      (await api.PATCH("/pages/{pageID}/tasks/{taskID}", { params: { path: { pageID: pageId, taskID: taskId } }, body: { done } })).data!.task,
    onSuccess: (_task, { pageId }) => {
      void queryClient.invalidateQueries({ queryKey: tasksQueryKey });
      void queryClient.invalidateQueries({ queryKey: pageQueryKey(pageId) });
    },
  });
}
