import { memo } from "react";
import {
  BarChart,
  Bar,
  XAxis,
  YAxis,
  CartesianGrid,
  Tooltip,
  Legend,
  ResponsiveContainer,
} from "recharts";

export interface CreditsEntry {
  customer: string;
  balance: number;
  spent: number;
  utility: number;
}

interface CreditsChartProps {
  data: CreditsEntry[];
}

export const CreditsChart = memo(({ data }: CreditsChartProps) => {
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
        Credits &amp; Utility
      </div>
      <div style={{ flex: 1, padding: "8px 4px 4px 0" }}>
        {data.length === 0 ? (
          <div
            style={{
              color: "#484f58",
              fontSize: 11,
              padding: "12px",
              textAlign: "center",
              fontFamily: "JetBrains Mono",
            }}
          >
            No credit data yet…
          </div>
        ) : (
          <ResponsiveContainer width="100%" height="100%">
            <BarChart data={data} margin={{ top: 4, right: 8, left: -10, bottom: 0 }}>
              <CartesianGrid strokeDasharray="3 3" stroke="#21262d" />
              <XAxis
                dataKey="customer"
                tick={{ fill: "#8b949e", fontSize: 9, fontFamily: "JetBrains Mono" }}
                tickLine={false}
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
              <Bar dataKey="balance" name="Balance" fill="#3fb950" radius={[3, 3, 0, 0]} />
              <Bar dataKey="spent" name="Spent" fill="#f85149" radius={[3, 3, 0, 0]} />
              <Bar dataKey="utility" name="Utility" fill="#00e5ff" radius={[3, 3, 0, 0]} />
            </BarChart>
          </ResponsiveContainer>
        )}
      </div>
    </div>
  );
});

CreditsChart.displayName = "CreditsChart";
