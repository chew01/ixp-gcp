import { memo, useEffect, useState } from "react";
import { getBezierPath, BaseEdge } from "@xyflow/react";
import type { EdgeProps, Edge } from "@xyflow/react";
import type { Packet } from "../types";

export interface AnimatedEdgeData extends Record<string, unknown> {
  label?: string;
  packets?: Packet[];
}

export type AnimatedEdgeType = Edge<AnimatedEdgeData, "animated">;

const TRAVEL_DURATION_MS = 1200;

export const AnimatedEdge = memo(
  ({
    id,
    sourceX,
    sourceY,
    targetX,
    targetY,
    sourcePosition,
    targetPosition,
    data,
  }: EdgeProps<AnimatedEdgeType>) => {
    const [edgePath, labelX, labelY] = getBezierPath({
      sourceX,
      sourceY,
      sourcePosition,
      targetX,
      targetY,
      targetPosition,
    });

    const packets = (data?.packets as Packet[] | undefined) ?? [];

    return (
      <>
        <BaseEdge
          id={id}
          path={edgePath}
          style={{ stroke: "#30363d", strokeWidth: 1.5 }}
          markerEnd="url(#arrowhead)"
        />

        {data?.label && (
          <text
            x={labelX}
            y={labelY - 8}
            textAnchor="middle"
            style={{
              fill: "#8b949e",
              fontSize: 9,
              fontFamily: "'JetBrains Mono', monospace",
              pointerEvents: "none",
            }}
          >
            {data.label as string}
          </text>
        )}

        {packets.map((pkt) => (
          <PacketDot key={pkt.id} path={edgePath} packet={pkt} />
        ))}
      </>
    );
  }
);

AnimatedEdge.displayName = "AnimatedEdge";

interface PacketDotProps {
  path: string;
  packet: Packet;
}

function PacketDot({ path, packet }: PacketDotProps) {
  const [opacity, setOpacity] = useState(1);

  useEffect(() => {
    const timer = setTimeout(
      () => setOpacity(0),
      TRAVEL_DURATION_MS * 0.75
    );
    return () => clearTimeout(timer);
  }, []);

  const age = Date.now() - packet.createdAt;
  if (age > TRAVEL_DURATION_MS) return null;

  return (
    <g
      style={{
        offsetPath: `path("${path}")`,
        offsetDistance: "0%",
        animation: `travel ${TRAVEL_DURATION_MS}ms linear forwards`,
        opacity,
        transition: "opacity 0.3s",
      }}
    >
      <circle r={5} fill={packet.color} />
      <text
        x={8}
        y={4}
        style={{
          fill: packet.color,
          fontSize: 8,
          fontFamily: "'JetBrains Mono', monospace",
          pointerEvents: "none",
          userSelect: "none",
        }}
      >
        {packet.label}
      </text>
    </g>
  );
}
