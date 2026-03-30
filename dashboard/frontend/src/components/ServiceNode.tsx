import { memo } from "react";
import { Handle, Position } from "@xyflow/react";
import type { NodeProps, Node } from "@xyflow/react";
import type { ServiceNodeData } from "../types";

export type ServiceNodeType = Node<ServiceNodeData, "service">;

const ROLE_ICONS: Record<string, string> = {
  "api-gateway": "⚡",
  "auction-runner": "🔨",
  "telemetry-service": "📡",
  atomix: "💾",
  kafka: "📨",
  agent: "🤖",
  dummy: "🎲",
};

const STATUS_COLORS: Record<string, string> = {
  healthy: "#3fb950",
  degraded: "#d29922",
  offline: "#f85149",
  unknown: "#8b949e",
};

export const ServiceNode = memo(({ data }: NodeProps<ServiceNodeType>) => {
  const icon = ROLE_ICONS[data.role] ?? "📦";
  const statusColor = STATUS_COLORS[data.status ?? "unknown"];
  const podText =
    data.podInfo != null
      ? `${data.podInfo.ready}/${data.podInfo.desired} ready`
      : null;

  return (
    <div
      style={{
        background: "#161b22",
        border: "1px solid #30363d",
        borderRadius: 10,
        padding: "10px 16px",
        minWidth: 140,
        fontFamily: "'Inter', sans-serif",
        boxShadow: "0 2px 12px rgba(0,0,0,0.5)",
      }}
    >
      <Handle type="target" position={Position.Left} style={{ opacity: 0 }} />
      <Handle type="source" position={Position.Right} style={{ opacity: 0 }} />
      <Handle type="target" position={Position.Top} style={{ opacity: 0 }} />
      <Handle type="source" position={Position.Bottom} style={{ opacity: 0 }} />

      <div style={{ display: "flex", alignItems: "center", gap: 6, marginBottom: 4 }}>
        <span style={{ fontSize: 16 }}>{icon}</span>
        <span
          style={{ color: statusColor, fontSize: 10, lineHeight: 1 }}
          title={data.status ?? "unknown"}
        >
          ●
        </span>
        <span
          style={{
            color: "#e6edf3",
            fontWeight: 600,
            fontSize: 12,
            whiteSpace: "nowrap",
          }}
        >
          {data.label}
        </span>
      </div>

      {podText && (
        <div style={{ color: "#8b949e", fontSize: 10 }}>{podText}</div>
      )}
      {data.meta && (
        <div style={{ color: "#00e5ff", fontSize: 10, marginTop: 2 }}>
          {data.meta}
        </div>
      )}
    </div>
  );
});

ServiceNode.displayName = "ServiceNode";
