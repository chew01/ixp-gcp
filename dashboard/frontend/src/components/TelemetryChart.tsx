import { memo } from "react";
import {
  LineChart,
  Line,
  XAxis,
  YAxis,
  CartesianGrid,
  Tooltip,
  Legend,
  ResponsiveContainer,
} from "recharts";

export interface TelemetryPoint {
  time: string;
  [flowKey: string]: string | number;
}

interface TelemetryChartProps {
  data: TelemetryPoint[];
  flowKeys: string[];
}

const FLOW_COLORS = ["#00e5ff", "#7ee787", "#ffa657", "#f78166", "#d2a8ff", "#79c0ff"];

interface TooltipProps {
  active?: boolean;
  payload?: Array<{ name: string; value: number; color: string }>;
  label?: string;
  data: TelemetryPoint[];
}

function CustomTooltip({ active, payload, label, data }: TooltipProps) {
  if (!active || !payload?.length) return null;

  const point = data.find((d) => d.time === label);

  const baseKeys = Array.from(
    new Set(payload.map((p) => p.name.replace(/ (in|eg)$/, "")))
  );

  return (
    <div
      style={{
        background: "#161b22",
        border: "1px solid #30363d",
        borderRadius: 6,
        padding: "8px 12px",
        fontSize: 11,
        fontFamily: "'JetBrains Mono', monospace",
        minWidth: 200,
      }}
    >
      <div style={{ color: "#8b949e", marginBottom: 6 }}>{label}</div>
      {baseKeys.map((base, i) => {
        const inEntry = payload.find((p) => p.name === `${base} in`);
        const egEntry = payload.find((p) => p.name === `${base} eg`);
        const drop = point ? (point[`${base} drop%`] as number | undefined) : undefined;
        const color = FLOW_COLORS[i % FLOW_COLORS.length];
        return (
          <div key={base} style={{ marginBottom: 4 }}>
            <div style={{ color, fontWeight: 600, marginBottom: 2 }}>{base}</div>
            {inEntry && (
              <div style={{ color: "#e6edf3", paddingLeft: 8 }}>
                in: {inEntry.value} kbps
              </div>
            )}
            {egEntry && (
              <div style={{ color: "#e6edf3", paddingLeft: 8 }}>
                eg: {egEntry.value} kbps
              </div>
            )}
            {drop != null && (
              <div style={{ color: drop > 5 ? "#f85149" : "#8b949e", paddingLeft: 8 }}>
                drop: {drop}%
              </div>
            )}
          </div>
        );
      })}
    </div>
  );
}

export const TelemetryChart = memo(({ data, flowKeys }: TelemetryChartProps) => {
  return (
    <div style={{ height: "100%", display: "flex", flexDirection: "column" }}>
      <div
        style={{
          padding: "8px 12px",
          borderBottom: "1px solid #30363d",
          color: "#8b949e",
          fontSize: 11,
          fontWeight: 600,
          letterSpacing: "0.05em",
          textTransform: "uppercase",
          fontFamily: "'JetBrains Mono', monospace",
        }}
      >
        Throughput (kbps)
      </div>
      <div style={{ flex: 1, padding: "8px 4px 4px 0" }}>
        <ResponsiveContainer width="100%" height="100%">
          <LineChart data={data} margin={{ top: 4, right: 8, left: -10, bottom: 0 }}>
            <CartesianGrid strokeDasharray="3 3" stroke="#21262d" />
            <XAxis
              dataKey="time"
              tick={{ fill: "#484f58", fontSize: 9, fontFamily: "JetBrains Mono" }}
              tickLine={false}
              interval="preserveStartEnd"
            />
            <YAxis
              tick={{ fill: "#484f58", fontSize: 9, fontFamily: "JetBrains Mono" }}
              tickLine={false}
              width={40}
            />
            <Tooltip content={<CustomTooltip data={data} />} />
            <Legend
              wrapperStyle={{
                fontSize: 10,
                fontFamily: "JetBrains Mono",
                color: "#8b949e",
              }}
            />
            {flowKeys.map((key, i) => (
              <Line
                key={key}
                type="monotone"
                dataKey={key}
                stroke={FLOW_COLORS[i % FLOW_COLORS.length]}
                dot={false}
                strokeWidth={1.5}
                isAnimationActive={false}
              />
            ))}
          </LineChart>
        </ResponsiveContainer>
      </div>
    </div>
  );
});

TelemetryChart.displayName = "TelemetryChart";
