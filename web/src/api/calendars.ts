import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { api } from "./client";
import type { components } from "./schema";

/** One of a space's calendars, and whether the reader may change it. */
export type Calendar = components["schemas"]["Calendar"];
/** One entry of a calendar: whole days at midnight UTC, or two instants. */
export type CalendarEvent = components["schemas"]["CalendarEvent"];
/** A new event, or all of one changed. */
export type CalendarEventInput = components["schemas"]["CalendarEventInput"];

export const calendarsQueryKey = ["calendars"] as const;

function spaceCalendarsKey(spaceKey: string) {
  return [...calendarsQueryKey, "space", spaceKey.toUpperCase()] as const;
}

function calendarKey(calendarId: string) {
  return [...calendarsQueryKey, "calendar", calendarId] as const;
}

/** A space's calendars by name; none asked for without a space. */
export function useCalendars(spaceKey: string | null) {
  return useQuery({
    queryKey: spaceCalendarsKey(spaceKey ?? ""),
    enabled: Boolean(spaceKey),
    queryFn: async (): Promise<Calendar[]> => (await api.GET("/spaces/{spaceKey}/calendars", { params: { path: { spaceKey: spaceKey! } } })).data!.calendars,
  });
}

export function useCreateCalendar() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: async ({ spaceKey, name }: { spaceKey: string; name: string }): Promise<Calendar> =>
      (await api.POST("/spaces/{spaceKey}/calendars", { params: { path: { spaceKey } }, body: { name } })).data!.calendar,
    onSuccess: (calendar) => queryClient.invalidateQueries({ queryKey: spaceCalendarsKey(calendar.spaceKey) }),
  });
}

/** A calendar and its events from one instant to another, as a month of the block asks. */
export function useCalendarEvents(calendarId: string, from: string, to: string) {
  return useQuery({
    queryKey: [...calendarKey(calendarId), from, to],
    enabled: calendarId !== "",
    queryFn: async () => (await api.GET("/calendars/{calendarID}/events", { params: { path: { calendarID: calendarId }, query: { from, to } } })).data!,
  });
}

/** Adds an event, or changes all of one when an id is given; every month of the calendar asks again. */
export function useSaveCalendarEvent(calendarId: string) {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: async ({ id, input }: { id: string | null; input: CalendarEventInput }): Promise<CalendarEvent> => {
      const path = { calendarID: calendarId };
      if (id === null) return (await api.POST("/calendars/{calendarID}/events", { params: { path }, body: input })).data!.event;
      return (await api.PUT("/calendars/{calendarID}/events/{eventID}", { params: { path: { ...path, eventID: id } }, body: input })).data!.event;
    },
    onSuccess: () => queryClient.invalidateQueries({ queryKey: calendarKey(calendarId) }),
  });
}

export function useDeleteCalendarEvent(calendarId: string) {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: async (id: string) => {
      await api.DELETE("/calendars/{calendarID}/events/{eventID}", { params: { path: { calendarID: calendarId, eventID: id } } });
    },
    onSuccess: () => queryClient.invalidateQueries({ queryKey: calendarKey(calendarId) }),
  });
}
