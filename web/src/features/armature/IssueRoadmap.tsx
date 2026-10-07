import { useContext, useMemo, type ReactNode } from "react";
import { ApiError } from "@/api/client";
import { useArmatureAccount, useArmatureRoadmap, type ArmatureRoadmapAnswer } from "@/api/armature";
import { ARMATURE_ROADMAP_LABEL_ROOM_PX, ARMATURE_SECTION_ID, PROFILE_PATH } from "@/config";
import { DocPageContext } from "@/features/editor/BlockViews";
import { locale, t } from "@/i18n";
import { BadQuery, Note, OpenInArmature } from "./IssueList";
import { axisOf, axisTicks, dayNumber, labelEvery, place, type RoadmapSettings, type Tick } from "./roadmap";
import { useWidth } from "./useWidth";

type Roadmap = NonNullable<ArmatureRoadmapAnswer["roadmap"]>;
type Bar = Roadmap["groups"][number]["rows"][number];
type Axis = { first: number; last: number };

// A bar keeps the colour Armature gives its status category, so the roadmap
// reads as the board does; the legend names each one.
const CATEGORIES = ["todo", "in_progress", "done"] as const;

function categoryOf(bar: Bar): string {
  return (CATEGORIES as readonly string[]).includes(bar.status.category) ? bar.status.category : "todo";
}

function dayLabel(day: string, withYear = true): string {
  return new Intl.DateTimeFormat(locale(), { day: "numeric", month: "short", year: withYear ? "numeric" : undefined, timeZone: "UTC" }).format(
    new Date(`${day}T00:00:00Z`),
  );
}

/** A month tick names its month, and its year where the axis starts or a year begins; a week tick its Monday. */
function tickLabel(tick: Tick, first: boolean): string {
  if (!tick.month) return dayLabel(tick.day, false);
  const year = first || tick.day.endsWith("-01-01") ? "numeric" : undefined;
  return new Intl.DateTimeFormat(locale(), { month: "short", year, timeZone: "UTC" }).format(new Date(`${tick.day}T00:00:00Z`));
}

/** What a bar says to a screen reader and in its tooltip: its days and where it stands. */
export function barWords(bar: Pick<Bar, "start" | "due" | "derived" | "status">): string {
  const r = t.armature.roadmap;
  const days =
    bar.start && bar.due ? r.span(dayLabel(bar.start), dayLabel(bar.due)) : bar.start ? r.startsOn(dayLabel(bar.start)) : r.dueOn(dayLabel(bar.due!));
  return [days, bar.derived ? r.derived : null, bar.status.name].filter(Boolean).join(", ");
}

function Track({ bar, axis, ticks, today }: { bar: Bar | null; axis: Axis; ticks: Tick[]; today: number | null }) {
  const at = bar ? place(bar.start ?? null, bar.due ?? null, axis) : null;
  return (
    <div className="doc-roadmap-track">
      {ticks.map((tick) => (
        <span key={tick.day} className="doc-roadmap-gridline" style={{ left: `${tick.at}%` }} aria-hidden="true" />
      ))}
      {today !== null && <span className="doc-roadmap-today" style={{ left: `${today}%` }} aria-hidden="true" />}
      {bar && at && (
        <>
          <span
            className="doc-roadmap-bar"
            style={{ left: `${at.left}%`, width: at.shape === "bar" ? `${at.width}%` : undefined }}
            title={barWords(bar)}
            data-shape={at.shape}
            data-category={categoryOf(bar)}
            data-derived={bar.derived || undefined}
            aria-hidden="true"
          />
          <span className="sr-only">{barWords(bar)}</span>
        </>
      )}
    </div>
  );
}

function Label({ bar, inEditor }: { bar: Bar; inEditor: boolean }) {
  // In the editor a link would take the author off the page mid-sentence.
  return (
    <div className="doc-roadmap-label">
      {inEditor ? (
        <span className="doc-roadmap-key">{bar.key}</span>
      ) : (
        <a href={bar.url} target="_blank" rel="noopener noreferrer" className="doc-roadmap-key">
          {bar.key}
        </a>
      )}{" "}
      <span className="doc-roadmap-summary" title={bar.summary}>
        {bar.summary}
      </span>
    </div>
  );
}

function Timeline({ roadmap, inEditor }: { roadmap: Roadmap; inEditor: boolean }) {
  const r = t.armature.roadmap;
  const axis = useMemo(() => axisOf(roadmap.from!, roadmap.to!), [roadmap.from, roadmap.to]);
  const ticks = useMemo(() => axisTicks(axis), [axis]);
  const todayN = dayNumber(new Date().toISOString().slice(0, 10));
  const today = todayN >= axis.first && todayN <= axis.last ? ((todayN - axis.first) / (axis.last - axis.first + 1)) * 100 : null;
  const { box, width } = useWidth(0);
  const every = labelEvery(ticks, width);
  const derived = roadmap.groups.some((g) => g.epic?.derived || g.rows.some((row) => row.derived));
  return (
    <div className="doc-roadmap-chart">
      <ul className="doc-chart-keys" data-roadmap-legend="">
        {CATEGORIES.map((c) => (
          <li key={c}>
            <span className="doc-roadmap-key-swatch" data-category={c} aria-hidden="true" />
            {r.categories[c]}
          </li>
        ))}
        {derived && (
          <li>
            <span className="doc-roadmap-key-swatch" data-category="todo" data-derived="true" aria-hidden="true" />
            {r.derivedKey}
          </li>
        )}
      </ul>
      <div className="doc-roadmap-row doc-roadmap-axis" aria-hidden="true">
        <div className="doc-roadmap-label" />
        <div className="doc-roadmap-track" ref={box}>
          {ticks.map((tick, i) => (
            <span key={tick.day} className="doc-roadmap-tick" style={{ left: `${tick.at}%` }}>
              {i % every === 0 && ((100 - tick.at) / 100) * width >= ARMATURE_ROADMAP_LABEL_ROOM_PX ? tickLabel(tick, i === 0) : null}
            </span>
          ))}
        </div>
      </div>
      {roadmap.groups.map((group, gi) => {
        const name = group.name || (roadmap.groupBy === "epic" ? r.noEpic : r.noTeam);
        return (
          <section
            key={group.epic?.key ?? `${gi}:${group.name}`}
            className="doc-roadmap-group"
            aria-label={name}
            data-roadmap-group={group.epic?.key ?? group.name}
          >
            <div className="doc-roadmap-row doc-roadmap-head">
              {group.epic ? <Label bar={group.epic} inEditor={inEditor} /> : <div className="doc-roadmap-label">{name}</div>}
              <Track bar={group.epic && (group.epic.start || group.epic.due) ? group.epic : null} axis={axis} ticks={ticks} today={today} />
            </div>
            <ul className="doc-roadmap-rows">
              {group.rows.map((bar) => (
                <li key={bar.key} className="doc-roadmap-row" data-roadmap-row={bar.key}>
                  <Label bar={bar} inEditor={inEditor} />
                  <Track bar={bar} axis={axis} ticks={ticks} today={today} />
                </li>
              ))}
            </ul>
          </section>
        );
      })}
    </div>
  );
}

/**
 * The issues an NQL query matches in one project on a timeline of their start
 * and due days, read with the reader's own token, so each reader sees what they may.
 */
export function IssueRoadmap({ settings, inEditor = false }: { settings: RoadmapSettings; inEditor?: boolean }) {
  const r = t.armature.roadmap;
  const l = t.armature.list;
  const page = useContext(DocPageContext);
  const edit = inEditor ? undefined : page?.onEdit;
  const account = useArmatureAccount();
  const configured = Boolean(account.data?.configured && account.data.baseUrl);
  const asks = Boolean(configured && account.data?.connected && account.data.status !== "rejected");
  const answer = useArmatureRoadmap(settings, asks);
  const roadmap = answer.data?.status === "ok" ? answer.data.roadmap : null;

  let state: string;
  let body: ReactNode;
  if (account.isPending) {
    state = "loading";
    body = <Note status>{r.loading}</Note>;
  } else if (!configured || answer.data?.status === "not_configured") {
    state = "plain";
    body = <Note>{l.notConfigured}</Note>;
  } else if (!asks || answer.data?.status === "not_connected" || answer.data?.status === "rejected") {
    state = "connect";
    body = (
      <Note>
        {r.connect}{" "}
        <a href={`${PROFILE_PATH}#${ARMATURE_SECTION_ID}`} className="underline" data-action="connect-armature-hint">
          {l.connectLink}
        </a>
      </Note>
    );
  } else if (answer.error instanceof ApiError && answer.error.code === "bad_query") {
    state = "bad_query";
    body = (
      <BadQuery query={settings.query} message={answer.error.message} position={answer.error.position} showQuery={inEditor || Boolean(edit)} onEdit={edit} />
    );
  } else if (answer.error instanceof ApiError && answer.error.fields.project) {
    state = "no_project";
    body = <Note>{answer.error.fields.project}</Note>;
  } else if (answer.isError) {
    state = "failed";
    body = <Note>{r.failed}</Note>;
  } else if (answer.isPending) {
    state = "loading";
    body = <Note status>{r.loading}</Note>;
  } else if (answer.data?.status === "unreachable" || !roadmap) {
    state = "unreachable";
    body = <Note>{l.unreachable}</Note>;
  } else if (roadmap.groups.length === 0 || !roadmap.from || !roadmap.to) {
    state = "empty";
    body = <Note>{roadmap.unscheduled > 0 ? r.allUnscheduled(roadmap.unscheduled) : r.empty}</Note>;
  } else {
    state = "roadmap";
    body = <Timeline roadmap={roadmap} inEditor={inEditor} />;
  }
  const left =
    roadmap && state === "roadmap" ? [roadmap.unscheduled > 0 && r.unscheduled(roadmap.unscheduled), roadmap.hidden > 0 && r.hidden(roadmap.hidden)] : [];
  return (
    <figure className="doc-chart doc-roadmap" data-armature-roadmap={settings.groupBy} data-state={state}>
      <figcaption className="doc-chart-title">{r.title(settings.project, settings.groupBy === "epic" ? r.byEpic : r.byTeam)}</figcaption>
      {body}
      {left.some(Boolean) && <p className="doc-roadmap-note">{left.filter(Boolean).join(" ")}</p>}
      {roadmap && !inEditor && roadmap.url && <OpenInArmature url={roadmap.url} />}
    </figure>
  );
}
