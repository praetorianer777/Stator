import { createRoute } from "@tanstack/react-router";
import { MyTasks } from "@/features/tasks/MyTasks";
import { appRoute } from "./app";

/** The caller's tasks across every page they may read; an assignment's notification leads to the page, this lists them all. */
export const tasksRoute = createRoute({ getParentRoute: () => appRoute, path: "/tasks", component: MyTasks });
