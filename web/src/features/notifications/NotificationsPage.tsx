import { useState } from "react";
import { Link } from "@tanstack/react-router";
import { useMarkRead, useNotificationPage, useUnreadCount } from "@/api/notifications";
import { Button, Card, Count, EmptyState, ErrorBanner, Page, PageHeader, Pager, Segmented, Skeleton, shownOf } from "@/components/ui";
import { Icon } from "@/components/icons";
import { NOTIFICATION_SETTINGS_PATH, NOTIFICATIONS_PAGE_SIZE } from "@/config";
import { t } from "@/i18n";
import { NotificationRow } from "./NotificationBell";

type View = "unread" | "all";

/** What the reader was told, newest first, as the bell lists it but all of it; opening one marks it read and goes there. */
export function NotificationsPage() {
  const [view, setView] = useState<View>("unread");
  const [offset, setOffset] = useState(0);
  const { data, error, isLoading, refetch } = useNotificationPage(view === "unread", offset);
  const unread = useUnreadCount().data ?? 0;
  const markRead = useMarkRead();
  const p = t.notifications.page;
  const items = data?.notifications ?? [];
  const total = data?.total ?? 0;

  return (
    <Page width="narrow" data-notifications-page="">
      <PageHeader
        title={t.notifications.title}
        meta={total > 0 ? shownOf(total, offset, items.length, p.noun) : undefined}
        actions={
          <Button
            variant="secondary"
            size="sm"
            disabled={unread === 0}
            loading={markRead.isPending}
            onClick={() => markRead.mutate({ all: true })}
            data-action="mark-all-read"
          >
            {t.notifications.markAllRead}
          </Button>
        }
      />
      <div className="mb-4 flex flex-wrap items-center justify-between gap-3">
        <Segmented<View>
          label={p.show}
          value={view}
          onChange={(next) => {
            setView(next);
            setOffset(0);
          }}
          options={[
            { value: "unread", label: p.unread, attrs: { "data-notifications-view": "unread" } },
            { value: "all", label: p.everything, attrs: { "data-notifications-view": "all" } },
          ]}
        />
        <Link to={NOTIFICATION_SETTINGS_PATH} className="text-sm text-accent hover:underline" data-action="notification-settings">
          {t.notifications.settings}
        </Link>
      </div>
      {error ? (
        <ErrorBanner onRetry={() => void refetch()}>{t.notifications.failed}</ErrorBanner>
      ) : isLoading ? (
        <Skeleton lines={6} />
      ) : items.length === 0 ? (
        <EmptyState icon={<Icon.Bell />} title={view === "unread" ? p.nothingUnread : p.nothingYet} description={p.emptyHint} />
      ) : (
        <>
          <Card className="p-1">
            <ul aria-label={t.notifications.title}>
              {items.map((n) => (
                <li key={n.id}>
                  <NotificationRow notification={n} className="px-3" />
                </li>
              ))}
            </ul>
          </Card>
          <Pager total={total} offset={offset} pageSize={NOTIFICATIONS_PAGE_SIZE} onChange={setOffset} label={p.pager} />
        </>
      )}
    </Page>
  );
}

/** How many notifications are unread, beside Notifications in the sidebar; nothing while none are. */
export function UnreadCount() {
  const unread = useUnreadCount().data ?? 0;
  return <Count count={unread} name="notifications" says={t.notifications.unreadSaid(unread)} />;
}
