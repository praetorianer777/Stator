import { createRoute } from "@tanstack/react-router";
import { AuditLog } from "@/features/audit/AuditLog";
import { adminRoute } from "./admin";

/** Who did what to the installation, newest first, for its administrators. */
export const auditRoute = createRoute({ getParentRoute: () => adminRoute, path: "/audit", component: AuditLog });
