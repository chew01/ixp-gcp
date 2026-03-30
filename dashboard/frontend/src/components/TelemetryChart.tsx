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
            <Tooltip
              contentStyle={{
                background: "#161b22",
                border: "1px solid #30363d",
                borderRadius: 6,
                fontSize: 11,
                fontFamily: "JetBrains Mono",
              }}
              labelStyle={{ color: "#8b949e" }}
            />
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
