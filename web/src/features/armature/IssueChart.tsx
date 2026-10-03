import { useContext, useEffect, useId, useMemo, useRef, useState, type KeyboardEvent, type PointerEvent, type ReactNode } from "react";
import { ApiError } from "@/api/client";
import { useArmatureAccount, useArmatureChart, type ArmatureChartAnswer } from "@/api/armature";
import {
  ARMATURE_FLOW_HEIGHT,
  ARMATURE_FLOW_TICKS,
  ARMATURE_FLOW_WIDTH,
  ARMATURE_PIE_HOLE,
  ARMATURE_PIE_SIZE,
  ARMATURE_SECTION_ID,
  PROFILE_PATH,
} from "@/config";
import { DocPageContext } from "@/features/editor/BlockViews";
import { locale, t } from "@/i18n";
import { BadQuery, Note, OpenInArmature } from "./IssueList";
import { donutPaths, foldSlices, linePoints, percent, readoutAnchor, ticks, type ChartSettings, type Slice } from "./chart";

type Chart = NonNullable<ArmatureChartAnswer["chart"]>;

// A status category keeps the colour Armature gives it, so a pie of
// categories reads as the board does; anything else takes the categorical
// slots in order, never by rank.
const CATEGORY_COLOR: Record<string, string> = {
  todo: "var(--color-status-todo)",
  in_progress: "var(--color-status-progress)",
  done: "var(--color-status-done)",
};

function sliceColor(slice: Slice, index: number, byCategory: boolean): string {
  if (slice.other) return "var(--chart-other)";
  if (byCategory && CATEGORY_COLOR[slice.category]) return CATEGORY_COLOR[slice.category]!;
  return `var(--chart-${index + 1})`;
}

function Pie({ chart }: { chart: Chart }) {
  const c = t.armature.chart;
  const slices = useMemo(() => foldSlices(chart.slices, undefined, c.other), [chart.slices, c.other]);
  const paths = useMemo(
    () =>
      donutPaths(
        slices.map((s) => s.count),
        ARMATURE_PIE_SIZE,
        ARMATURE_PIE_HOLE,
      ),
    [slices],
  );
  const [active, setActive] = useState<number | null>(null);
  const byCategory = chart.groupBy === "statusCategory" || chart.groupBy === "status";
  const shown = active !== null ? slices[active] : null;
  const grouping = c.groupings[chart.groupBy] ?? chart.groupBy;
  return (
    <div className="doc-chart-pie">
      <svg
        viewBox={`0 0 ${ARMATURE_PIE_SIZE} ${ARMATURE_PIE_SIZE}`}
        className="doc-chart-donut"
        role="img"
        aria-label={c.pieSummary(grouping, chart.total)}
        onPointerLeave={() => setActive(null)}
      >
        {paths.map((d, i) => (
          <path
            key={slices[i]!.label}
            d={d}
            fill={sliceColor(slices[i]!, i, byCategory)}
            className="doc-chart-slice"
            data-active={active === null ? undefined : active === i}
            onPointerEnter={() => setActive(i)}
            data-chart-slice={slices[i]!.label}
          />
        ))}
        {/* The middle reads the slice pointed at, or the whole when none is. */}
        <text x="50%" y="47%" textAnchor="middle" className="doc-chart-hole-value">
          {shown ? shown.count : chart.total}
        </text>
        <text x="50%" y="60%" textAnchor="middle" className="doc-chart-hole-label">
          {shown ? `${percent(shown.count, chart.total)}%` : c.issues}
        </text>
      </svg>
      <table className="doc-chart-legend" data-chart-legend="">
        <caption className="sr-only">{c.legendCaption(grouping)}</caption>
        <thead className="sr-only">
          <tr>
            <th scope="col">{grouping}</th>
            <th scope="col">{c.count}</th>
            <th scope="col">{c.share}</th>
          </tr>
        </thead>
        <tbody>
          {slices.map((slice, i) => (
            <tr
              key={slice.label}
              data-active={active === i || undefined}
              onPointerEnter={() => setActive(i)}
              onPointerLeave={() => setActive(null)}
              data-legend-row={slice.label}
            >
              <th scope="row">
                <span className="doc-chart-swatch" style={{ background: sliceColor(slice, i, byCategory) }} aria-hidden="true" />
                {slice.label}
              </th>
              <td>{slice.count}</td>
              <td>{percent(slice.count, chart.total)}%</td>
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  );
}

function dayLabel(day: string): string {
  return new Intl.DateTimeFormat(locale(), { day: "numeric", month: "short", timeZone: "UTC" }).format(new Date(`${day}T00:00:00Z`));
}

const PAD = { left: 32, right: 12, top: 12, bottom: 24 };
// How wide the readout is taken to be when it is kept inside the plot.
const READOUT_WIDTH = 120;

/**
 * The plot's width in pixels, so the drawing is one unit to the pixel and its
 * words keep their size on a phone rather than shrinking with the drawing.
 */
function useWidth(fallback: number) {
  const box = useRef<HTMLDivElement>(null);
  const [width, setWidth] = useState(fallback);
  useEffect(() => {
    const el = box.current;
    if (!el || typeof ResizeObserver === "undefined") return;
    const seen = new ResizeObserver(([entry]) => {
      const w = Math.round(entry?.contentRect.width ?? 0);
      if (w > 0) setWidth(w);
    });
    seen.observe(el);
    return () => seen.disconnect();
  }, []);
  return { box, width };
}

function Flow({ chart, days }: { chart: Chart; days: number }) {
  const c = t.armature.chart;
  const id = useId();
  const [active, setActive] = useState<number | null>(null);
  const { box, width: plotWidth } = useWidth(ARMATURE_FLOW_WIDTH);
  const created = chart.days.map((d) => d.created);
  const resolved = chart.days.map((d) => d.resolved);
  const top = ticks(Math.max(1, ...created, ...resolved), ARMATURE_FLOW_TICKS);
  const highest = top[top.length - 1]!;
  const width = plotWidth - PAD.left - PAD.right;
  const height = ARMATURE_FLOW_HEIGHT - PAD.top - PAD.bottom;
  const line = (values: number[]) =>
    linePoints(values, width, height, highest)
      .map(([x, y]) => `${(x + PAD.left).toFixed(1)},${(y + PAD.top).toFixed(1)}`)
      .join(" ");
  const step = chart.days.length > 1 ? width / (chart.days.length - 1) : 0;
  const nearest = (event: PointerEvent<SVGSVGElement>) => {
    const box = event.currentTarget.getBoundingClientRect();
    const x = ((event.clientX - box.left) / box.width) * plotWidth - PAD.left;
    return Math.min(chart.days.length - 1, Math.max(0, Math.round(step > 0 ? x / step : 0)));
  };
  // The arrows walk the days, so the keyboard reads what the pointer does.
  const onKeyDown = (event: KeyboardEvent<SVGSVGElement>) => {
    const move = { ArrowLeft: -1, ArrowRight: 1, Home: -Infinity, End: Infinity }[event.key];
    if (move === undefined) return;
    event.preventDefault();
    setActive((was) => Math.min(chart.days.length - 1, Math.max(0, (was ?? chart.days.length - 1) + move)));
  };
  const shown = active !== null ? chart.days[active] : null;
  const totalCreated = created.reduce((a, b) => a + b, 0);
  const totalResolved = resolved.reduce((a, b) => a + b, 0);
  const xAt = (i: number) => PAD.left + i * step;
  const labelled = chart.days.length > 0 ? [0, Math.floor((chart.days.length - 1) / 2), chart.days.length - 1] : [];
  return (
    <div className="doc-chart-flow">
      <ul className="doc-chart-keys" data-chart-legend="">
        <li>
          <span className="doc-chart-key" style={{ background: "var(--chart-1)" }} aria-hidden="true" />
          {c.created(totalCreated)}
        </li>
        <li>
          <span className="doc-chart-key" style={{ background: "var(--chart-2)" }} aria-hidden="true" />
          {c.resolved(totalResolved)}
        </li>
      </ul>
      <div className="doc-chart-plot" ref={box}>
        <svg
          viewBox={`0 0 ${plotWidth} ${ARMATURE_FLOW_HEIGHT}`}
          role="img"
          aria-label={c.flowSummary(days, totalCreated, totalResolved)}
          aria-describedby={shown ? `${id}-readout` : undefined}
          tabIndex={0}
          className="doc-chart-lines"
          onPointerMove={(event) => setActive(nearest(event))}
          onPointerLeave={() => setActive(null)}
          onKeyDown={onKeyDown}
          onBlur={() => setActive(null)}
          data-chart-flow=""
        >
          {top.map((v) => {
            const y = PAD.top + height - (v / highest) * height;
            return (
              <g key={v}>
                <line x1={PAD.left} x2={PAD.left + width} y1={y} y2={y} className="doc-chart-grid" />
                <text x={PAD.left - 6} y={y + 4} textAnchor="end" className="doc-chart-tick">
                  {v}
                </text>
              </g>
            );
          })}
          {labelled.map((i) => (
            <text
              key={i}
              x={xAt(i)}
              y={ARMATURE_FLOW_HEIGHT - 6}
              textAnchor={i === 0 ? "start" : i === chart.days.length - 1 ? "end" : "middle"}
              className="doc-chart-tick"
            >
              {dayLabel(chart.days[i]!.day)}
            </text>
          ))}
          {shown && <line x1={xAt(active!)} x2={xAt(active!)} y1={PAD.top} y2={PAD.top + height} className="doc-chart-crosshair" />}
          <polyline points={line(created)} className="doc-chart-line" stroke="var(--chart-1)" data-series="created" />
          <polyline points={line(resolved)} className="doc-chart-line" stroke="var(--chart-2)" data-series="resolved" />
          {shown && (
            <>
              <circle cx={xAt(active!)} cy={PAD.top + height - (shown.created / highest) * height} r={4} fill="var(--chart-1)" className="doc-chart-dot" />
              <circle cx={xAt(active!)} cy={PAD.top + height - (shown.resolved / highest) * height} r={4} fill="var(--chart-2)" className="doc-chart-dot" />
            </>
          )}
        </svg>
        {shown && (
          <div
            id={`${id}-readout`}
            className="doc-chart-tooltip"
            role="status"
            style={{ left: `${(xAt(active!) / plotWidth) * 100}%` }}
            data-anchor={readoutAnchor(xAt(active!), plotWidth, READOUT_WIDTH)}
            data-chart-readout={shown.day}
          >
            <span className="doc-chart-tooltip-day">{dayLabel(shown.day)}</span>
            <span>
              <span className="doc-chart-key" style={{ background: "var(--chart-1)" }} aria-hidden="true" />
              <strong>{shown.created}</strong> {c.createdWord}
            </span>
            <span>
              <span className="doc-chart-key" style={{ background: "var(--chart-2)" }} aria-hidden="true" />
              <strong>{shown.resolved}</strong> {c.resolvedWord}
            </span>
          </div>
        )}
      </div>
      <details className="doc-chart-table">
        <summary>{c.showTable}</summary>
        <table>
          <caption className="sr-only">{c.flowSummary(days, totalCreated, totalResolved)}</caption>
          <thead>
            <tr>
              <th scope="col">{c.day}</th>
              <th scope="col">{c.createdWord}</th>
              <th scope="col">{c.resolvedWord}</th>
            </tr>
          </thead>
          <tbody>
            {chart.days.map((d) => (
              <tr key={d.day}>
                <th scope="row">{dayLabel(d.day)}</th>
                <td>{d.created}</td>
                <td>{d.resolved}</td>
              </tr>
            ))}
          </tbody>
        </table>
      </details>
    </div>
  );
}

/**
 * A chart of the issues an NQL query matches in one project, counted by
 * Armature with the reader's own token, so each reader counts what they may see.
 */
export function IssueChart({ settings, inEditor = false }: { settings: ChartSettings; inEditor?: boolean }) {
  const c = t.armature.chart;
  const l = t.armature.list;
  const page = useContext(DocPageContext);
  const edit = inEditor ? undefined : page?.onEdit;
  const account = useArmatureAccount();
  const configured = Boolean(account.data?.configured && account.data.baseUrl);
  const asks = Boolean(configured && account.data?.connected && account.data.status !== "rejected");
  const answer = useArmatureChart(settings, asks);
  const chart = answer.data?.status === "ok" ? answer.data.chart : null;

  let state: string;
  let body: ReactNode;
  if (account.isPending) {
    state = "loading";
    body = <Note status>{c.loading}</Note>;
  } else if (!configured || answer.data?.status === "not_configured") {
    state = "plain";
    body = <Note>{l.notConfigured}</Note>;
  } else if (!asks || answer.data?.status === "not_connected" || answer.data?.status === "rejected") {
    state = "connect";
    body = (
      <Note>
        {c.connect}{" "}
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
    body = <Note>{c.failed}</Note>;
  } else if (answer.isPending) {
    state = "loading";
    body = <Note status>{c.loading}</Note>;
  } else if (answer.data?.status === "unreachable" || !chart) {
    state = "unreachable";
    body = <Note>{l.unreachable}</Note>;
  } else if (settings.chart === "pie" ? chart.total === 0 : chart.days.every((d) => d.created === 0 && d.resolved === 0)) {
    state = "empty";
    body = <Note>{settings.chart === "pie" ? c.emptyPie : c.emptyFlow(settings.days)}</Note>;
  } else {
    state = "chart";
    body = settings.chart === "pie" ? <Pie chart={chart} /> : <Flow chart={chart} days={settings.days} />;
  }
  const title =
    settings.chart === "pie" ? c.pieTitle(settings.project, c.groupings[settings.groupBy] ?? settings.groupBy) : c.flowTitle(settings.project, settings.days);
  return (
    <figure className="doc-chart" data-armature-chart={settings.chart} data-state={state}>
      <figcaption className="doc-chart-title">{title}</figcaption>
      {body}
      {chart && !inEditor && chart.url && <OpenInArmature url={chart.url} />}
    </figure>
  );
}
