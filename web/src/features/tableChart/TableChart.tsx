import { useId, useMemo, useState, type KeyboardEvent, type PointerEvent, type ReactNode } from "react";
import { ARMATURE_PIE_HOLE, ARMATURE_PIE_SIZE, TABLE_CHART_BAR_GAP, TABLE_CHART_HEIGHT, TABLE_CHART_TICKS, TABLE_CHART_WIDTH } from "@/config";
import { donutPaths, foldSlices, percent, readoutAnchor } from "@/features/armature/chart";
import { useWidth } from "@/features/armature/useWidth";
import type { DocNode } from "@/features/editor/schema";
import { t } from "@/i18n";
import { formatNumber } from "@/lib/format";
import { axisTicks, chartData, yOf, type ChartData, type TableChartKind } from "./data";

const PAD = { left: 44, right: 12, top: 12, bottom: 28 };
// How wide the readout is taken to be when it is kept inside the plot.
const READOUT_WIDTH = 140;

// Colour follows the series' place in the table, never its rank.
const color = (i: number) => `var(--chart-${i + 1})`;

function Keys({ data }: { data: ChartData }) {
  if (data.series.length < 2) return null;
  return (
    <ul className="doc-chart-keys" data-chart-legend="">
      {data.series.map((s, i) => (
        <li key={i}>
          <span className="doc-chart-key" style={{ background: color(i) }} aria-hidden="true" />
          {s.name}
        </li>
      ))}
    </ul>
  );
}

/** The numbers drawn, as a table for whoever cannot read the picture. */
function DataTable({ data, summary }: { data: ChartData; summary: string }) {
  const c = t.tableChart;
  return (
    <details className="doc-chart-table">
      <summary>{c.showData}</summary>
      <table>
        <caption className="sr-only">{summary}</caption>
        <thead>
          <tr>
            <th scope="col">{c.category}</th>
            {data.series.map((s, i) => (
              <th key={i} scope="col">
                {s.name}
              </th>
            ))}
          </tr>
        </thead>
        <tbody>
          {data.categories.map((name, row) => (
            <tr key={row}>
              <th scope="row">{name}</th>
              {data.series.map((s, i) => (
                <td key={i}>{s.values[row] === null ? "" : formatNumber(s.values[row]!)}</td>
              ))}
            </tr>
          ))}
        </tbody>
      </table>
    </details>
  );
}

/** Bars or lines over the categories, on one axis from the lowest value or 0 to the highest. */
function Plot({ data, kind, summary }: { data: ChartData; kind: "bar" | "line"; summary: string }) {
  const id = useId();
  const [active, setActive] = useState<number | null>(null);
  const { box, width: plotWidth } = useWidth(TABLE_CHART_WIDTH);
  const values = data.series.flatMap((s) => s.values.filter((v): v is number => v !== null));
  const ticks = axisTicks(Math.min(...values), Math.max(...values), TABLE_CHART_TICKS);
  const width = plotWidth - PAD.left - PAD.right;
  const height = TABLE_CHART_HEIGHT - PAD.top - PAD.bottom;
  const count = data.categories.length;
  const slot = count > 0 ? width / count : width;
  const y = (v: number) => PAD.top + yOf(v, ticks, height);
  // Bars sit in a slot each; a line's points sit at the slots' middles, so the two read alike.
  const xAt = (i: number) => PAD.left + slot * (i + 0.5);
  const nearest = (event: PointerEvent<SVGSVGElement>) => {
    const rect = event.currentTarget.getBoundingClientRect();
    const x = ((event.clientX - rect.left) / rect.width) * plotWidth - PAD.left;
    return Math.min(count - 1, Math.max(0, Math.floor(x / slot)));
  };
  const onKeyDown = (event: KeyboardEvent<SVGSVGElement>) => {
    const move = { ArrowLeft: -1, ArrowRight: 1, Home: -Infinity, End: Infinity }[event.key];
    if (move === undefined) return;
    event.preventDefault();
    setActive((was) => Math.min(count - 1, Math.max(0, (was ?? -1) + move)));
  };
  const group = slot * (1 - TABLE_CHART_BAR_GAP * 2);
  // A 2px gap of the surface between neighbouring bars.
  const bar = Math.max(1, group / data.series.length - 2);
  const zero = y(0);
  // Every label when they fit, else every few, so none run into another.
  const every = Math.max(1, Math.ceil(count / Math.max(1, Math.floor(width / 64))));
  return (
    <div className="doc-chart-flow">
      <Keys data={data} />
      <div className="doc-chart-plot" ref={box}>
        <svg
          viewBox={`0 0 ${plotWidth} ${TABLE_CHART_HEIGHT}`}
          role="img"
          aria-label={summary}
          aria-describedby={active !== null ? `${id}-readout` : undefined}
          tabIndex={0}
          className="doc-chart-lines"
          onPointerMove={(event) => setActive(nearest(event))}
          onPointerLeave={() => setActive(null)}
          onKeyDown={onKeyDown}
          onBlur={() => setActive(null)}
          data-table-chart-plot={kind}
        >
          {ticks.map((v) => (
            <g key={v}>
              <line x1={PAD.left} x2={PAD.left + width} y1={y(v)} y2={y(v)} className="doc-chart-grid" />
              <text x={PAD.left - 6} y={y(v) + 4} textAnchor="end" className="doc-chart-tick">
                {formatNumber(v)}
              </text>
            </g>
          ))}
          {data.categories.map((name, i) =>
            i % every === 0 ? (
              <text key={i} x={xAt(i)} y={TABLE_CHART_HEIGHT - 8} textAnchor="middle" className="doc-chart-tick">
                {name}
              </text>
            ) : null,
          )}
          {active !== null && kind === "line" && <line x1={xAt(active)} x2={xAt(active)} y1={PAD.top} y2={PAD.top + height} className="doc-chart-crosshair" />}
          {kind === "bar"
            ? data.series.map((s, si) =>
                s.values.map((v, i) => {
                  if (v === null) return null;
                  const x = PAD.left + slot * i + slot * TABLE_CHART_BAR_GAP + (group / data.series.length) * si + 1;
                  const top = Math.min(y(v), zero);
                  return (
                    <rect
                      key={`${si}-${i}`}
                      x={x}
                      y={top}
                      width={bar}
                      height={Math.max(1, Math.abs(zero - y(v)))}
                      rx={Math.min(4, bar / 2)}
                      fill={color(si)}
                      className="doc-chart-bar"
                      data-active={active === null ? undefined : active === i}
                      data-bar={`${s.name}:${data.categories[i]}`}
                    />
                  );
                }),
              )
            : data.series.map((s, si) => {
                // A cell with no number breaks the line rather than drawing a zero.
                const runs: string[][] = [[]];
                s.values.forEach((v, i) => {
                  if (v === null) runs.push([]);
                  else runs[runs.length - 1]!.push(`${xAt(i).toFixed(1)},${y(v).toFixed(1)}`);
                });
                return runs
                  .filter((run) => run.length > 0)
                  .map((run, ri) =>
                    run.length === 1 ? (
                      <circle key={`${si}-${ri}`} cx={run[0]!.split(",")[0]} cy={run[0]!.split(",")[1]} r={4} fill={color(si)} />
                    ) : (
                      <polyline key={`${si}-${ri}`} points={run.join(" ")} className="doc-chart-line" stroke={color(si)} data-series={s.name} />
                    ),
                  );
              })}
          {active !== null &&
            kind === "line" &&
            data.series.map((s, si) =>
              s.values[active] === null ? null : (
                <circle key={si} cx={xAt(active)} cy={y(s.values[active]!)} r={4} fill={color(si)} className="doc-chart-dot" />
              ),
            )}
        </svg>
        {active !== null && (
          <div
            id={`${id}-readout`}
            className="doc-chart-tooltip"
            role="status"
            style={{ left: `${(xAt(active) / plotWidth) * 100}%` }}
            data-anchor={readoutAnchor(xAt(active), plotWidth, READOUT_WIDTH)}
            data-chart-readout={data.categories[active]}
          >
            <span className="doc-chart-tooltip-day">{data.categories[active]}</span>
            {data.series.map((s, si) => (
              <span key={si}>
                <span className="doc-chart-key" style={{ background: color(si) }} aria-hidden="true" />
                <strong>{s.values[active] === null ? t.tableChart.none : formatNumber(s.values[active]!)}</strong> {s.name}
              </span>
            ))}
          </div>
        )}
      </div>
    </div>
  );
}

/** The first series as shares of its whole; only what is more than nothing has a share. */
function Pie({ data, summary }: { data: ChartData; summary: string }) {
  const c = t.tableChart;
  const series = data.series[0]!;
  const slices = useMemo(
    () =>
      foldSlices(
        data.categories.map((label, i) => ({ label, category: "", count: series.values[i] ?? 0 })).filter((s) => s.count > 0),
        undefined,
        c.other,
      ),
    [data.categories, series.values, c.other],
  );
  const total = slices.reduce((n, s) => n + s.count, 0);
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
  const shown = active !== null ? slices[active] : null;
  const fill = (i: number) => (slices[i]!.other ? "var(--chart-other)" : color(i));
  return (
    <div className="doc-chart-pie">
      <svg
        viewBox={`0 0 ${ARMATURE_PIE_SIZE} ${ARMATURE_PIE_SIZE}`}
        className="doc-chart-donut"
        role="img"
        aria-label={summary}
        onPointerLeave={() => setActive(null)}
      >
        {paths.map((d, i) => (
          <path
            key={slices[i]!.label + i}
            d={d}
            fill={fill(i)}
            className="doc-chart-slice"
            data-active={active === null ? undefined : active === i}
            onPointerEnter={() => setActive(i)}
            data-chart-slice={slices[i]!.label}
          />
        ))}
        <text x="50%" y="47%" textAnchor="middle" className="doc-chart-hole-value">
          {formatNumber(shown ? shown.count : total)}
        </text>
        <text x="50%" y="60%" textAnchor="middle" className="doc-chart-hole-label">
          {shown ? `${percent(shown.count, total)}%` : series.name}
        </text>
      </svg>
      <table className="doc-chart-legend" data-chart-legend="">
        <caption className="sr-only">{summary}</caption>
        <thead className="sr-only">
          <tr>
            <th scope="col">{c.category}</th>
            <th scope="col">{series.name}</th>
            <th scope="col">{c.share}</th>
          </tr>
        </thead>
        <tbody>
          {slices.map((slice, i) => (
            <tr
              key={slice.label + i}
              data-active={active === i || undefined}
              onPointerEnter={() => setActive(i)}
              onPointerLeave={() => setActive(null)}
              data-legend-row={slice.label}
            >
              <th scope="row">
                <span className="doc-chart-swatch" style={{ background: fill(i) }} aria-hidden="true" />
                {slice.label}
              </th>
              <td>{formatNumber(slice.count)}</td>
              <td>{percent(slice.count, total)}%</td>
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  );
}

/**
 * A table drawn as a chart: the first row names the series, the first column
 * the categories. The table is read afresh on every draw, so the chart follows it.
 */
export function TableChart({ table, kind, dataTable = true }: { table: DocNode | undefined; kind: TableChartKind; dataTable?: boolean }) {
  const c = t.tableChart;
  const data = useMemo(() => chartData(table), [table]);
  const pieTotal = data.series[0]?.values.reduce<number>((n, v) => n + (v !== null && v > 0 ? v : 0), 0) ?? 0;
  let state: string;
  let body: ReactNode;
  // A pie draws the first series alone, so it is named alone.
  const drawn = kind === "pie" ? data.series.slice(0, 1) : data.series;
  const summary = c.summary(
    kind,
    drawn.map((s) => s.name),
    data.categories.length,
  );
  if (data.series.length === 0 || data.categories.length === 0) {
    state = "empty";
    body = <p className="doc-block-empty">{c.empty}</p>;
  } else if (kind === "pie" && pieTotal <= 0) {
    state = "empty";
    body = <p className="doc-block-empty">{c.emptyPie}</p>;
  } else {
    state = "chart";
    body = (
      <>
        {kind === "pie" ? <Pie data={data} summary={summary} /> : <Plot data={data} kind={kind} summary={summary} />}
        {kind === "pie" && data.series.length > 1 && <p className="doc-roadmap-note">{c.pieFirst(data.series[0]!.name)}</p>}
        {data.dropped > 0 && <p className="doc-roadmap-note">{c.dropped(data.dropped)}</p>}
        {dataTable && kind !== "pie" && <DataTable data={data} summary={summary} />}
      </>
    );
  }
  return (
    <figure className="doc-chart" data-table-chart={kind} data-state={state}>
      <figcaption className="doc-chart-title">{data.series.length > 0 ? c.title(drawn.map((s) => s.name)) : c.untitled}</figcaption>
      {body}
    </figure>
  );
}
