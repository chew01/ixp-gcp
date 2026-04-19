import { memo, useEffect, useState } from "react";
import { getBezierPath, BaseEdge } from "@xyflow/react";
import type { EdgeProps, Edge } from "@xyflow/react";
import type { Packet } from "../types";

export interface AnimatedEdgeData extends Record<string, unknown> {
  label?: string;
  packets?: Packet[];
  /** Y-offset applied to source/target to separate bidirectional edge pairs. */
  offset?: number;
  /** Render arrowheads on both ends (single line, both directions). */
  bidirectional?: boolean;
}

export type AnimatedEdgeType = Edge<AnimatedEdgeData, "animated">;

const TRAVEL_DURATION_MS = 1200;

// Arrowhead color matches the edge stroke.
const ARROW_COLOR = "#8b949e";

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
    const off = (data?.offset as number | undefined) ?? 0;
    const [edgePath, labelX, labelY] = getBezierPath({
      sourceX,
      sourceY: sourceY + off,
      sourcePosition,
      targetX,
      targetY: targetY + off,
      targetPosition,
    });

    const packets = (data?.packets as Packet[] | undefined) ?? [];

    return (
      <>
        {/* Define arrowhead markers once per edge (browser deduplicates by ID). */}
        <defs>
          <marker
            id="ixp-arrow"
            markerWidth="8"
            markerHeight="6"
            refX="7"
            refY="3"
            orient="auto"
          >
            <polygon points="0 0, 8 3, 0 6" fill={ARROW_COLOR} />
          </marker>
          <marker
            id="ixp-arrow-rev"
            markerWidth="8"
            markerHeight="6"
            refX="7"
            refY="3"
            orient="auto-start-reverse"
          >
            <polygon points="0 0, 8 3, 0 6" fill={ARROW_COLOR} />
          </marker>
        </defs>

        <BaseEdge
          id={id}
          path={edgePath}
          style={{ stroke: "#30363d", strokeWidth: 1.5 }}
          markerEnd="url(#ixp-arrow)"
          markerStart={data?.bidirectional ? "url(#ixp-arrow-rev)" : undefined}
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
        offsetDistance: packet.reversed ? "100%" : "0%",
        animation: `${packet.reversed ? "travel-reverse" : "travel"} ${TRAVEL_DURATION_MS}ms linear forwards`,
        opacity,
        transition: "opacity 0.3s",
      }}
    >
      <circle r={5} fill={packet.color} />
    </g>
  );
}
