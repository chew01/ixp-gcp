import { useCallback, useEffect } from "react";
import {
  ReactFlow,
  Background,
  Controls,
  MiniMap,
  addEdge,
  useNodesState,
  useEdgesState,
} from "@xyflow/react";
import type { Connection } from "@xyflow/react";
import "@xyflow/react/dist/style.css";

import { ServiceNode } from "./ServiceNode";
import type { ServiceNodeType } from "./ServiceNode";
import { AnimatedEdge } from "./AnimatedEdge";
import type { AnimatedEdgeType } from "./AnimatedEdge";
import type {
  Scenario,
  PodsPayload,
  Packet,
  ServiceNodeData,
  WSEvent,
  BidPayload,
  AtomixRWPayload,
  AuctionPayload,
  TelemetryPayload,
} from "../types";

const nodeTypes = { service: ServiceNode };
const edgeTypes = { animated: AnimatedEdge };

// Build the initial nodes and edges from the loaded scenario.
function buildGraph(scenario: Scenario): {
  nodes: ServiceNodeType[];
  edges: AnimatedEdgeType[];
} {
  const nodes: ServiceNodeType[] = [];
  const edges: AnimatedEdgeType[] = [];

  const X_AGENTS = 50;
  const X_API = 350;
  const X_ATOMIX = 700;
  const X_AUCTION = 350;
  const X_TELEMETRY = 550;
  const X_KAFKA = 700;
  const X_DUMMY = 150;
  const Y_TOP = 80;
  const Y_MID = 280;
  const Y_BOT = 460;

  const customers = Array.from(
    new Set((scenario.customers ?? []).map((c) => c.id))
  );

  customers.forEach((cid, i) => {
    const yPos = Y_TOP + i * 110;
    nodes.push({
      id: `agent:${cid}`,
      type: "service" as const,
      position: { x: X_AGENTS, y: yPos },
      data: { label: cid, role: "agent", status: "unknown", customerId: cid } as ServiceNodeData,
    });

    edges.push({
      id: `agent:${cid}--api-gateway--POST /bids`,
      type: "animated" as const,
      source: `agent:${cid}`,
      sourceHandle: "right-out",
      target: "api-gateway",
      targetHandle: "left-in",
      data: { label: "POST /bids", packets: [], offset: -12 },
    });
    edges.push({
      id: `api-gateway--agent:${cid}--GET /flows`,
      type: "animated" as const,
      source: "api-gateway",
      sourceHandle: "left-out",
      target: `agent:${cid}`,
      targetHandle: "right-in",
      data: { label: "GET /flows", packets: [], offset: 12 },
    });
  });

  const apiY = Math.max(Y_TOP, ((customers.length - 1) / 2) * 110 + Y_TOP);
  nodes.push({
    id: "api-gateway",
    type: "service" as const,
    position: { x: X_API, y: apiY },
    data: { label: "API Gateway", role: "api-gateway", status: "unknown" } as ServiceNodeData,
  });
  edges.push({
    id: "api-gateway--atomix--read/write",
    type: "animated" as const,
    source: "api-gateway",
    sourceHandle: "right-out",
    target: "atomix",
    targetHandle: "left-in",
    data: { label: "read/write", packets: [], bidirectional: true },
  });

  nodes.push({
    id: "auction-runner",
    type: "service" as const,
    position: { x: X_AUCTION, y: Y_MID },
    data: { label: "Auction Runner", role: "auction-runner", status: "unknown" } as ServiceNodeData,
  });
  edges.push({
    id: "auction-runner--atomix--write",
    type: "animated" as const,
    source: "auction-runner",
    sourceHandle: "right-out",
    target: "atomix",
    targetHandle: "left-in",
    data: { label: "write auction result", packets: [], offset: -12 },
  });
  edges.push({
    id: "atomix--auction-runner--read",
    type: "animated" as const,
    source: "atomix",
    sourceHandle: "left-out",
    target: "auction-runner",
    targetHandle: "right-in",
    data: { label: "read bids", packets: [], offset: 12 },
  });
  edges.push({
    id: "auction-runner--kafka--auction-results",
    type: "animated" as const,
    source: "auction-runner",
    target: "kafka",
    data: { label: "produce auction-results", packets: [] },
  });

  nodes.push({
    id: "telemetry-service",
    type: "service" as const,
    position: { x: X_TELEMETRY, y: Y_BOT },
    data: { label: "Telemetry", role: "telemetry-service", status: "unknown" } as ServiceNodeData,
  });
  edges.push({
    id: "telemetry-service--atomix--write",
    type: "animated" as const,
    source: "telemetry-service",
    target: "atomix",
    data: { label: "write throughput", packets: [] },
  });
  edges.push({
    id: "kafka--telemetry-service--consume",
    type: "animated" as const,
    source: "kafka",
    target: "telemetry-service",
    data: { label: "consume switch-telemetry", packets: [] },
  });

  nodes.push({
    id: "dummy-producer",
    type: "service" as const,
    position: { x: X_DUMMY, y: Y_BOT },
    data: { label: "Switch", role: "dummy", status: "unknown" } as ServiceNodeData,
  });
  edges.push({
    id: "dummy-producer--kafka--produce",
    type: "animated" as const,
    source: "dummy-producer",
    sourceHandle: "right-out",
    target: "kafka",
    targetHandle: "left-in",
    data: { label: "produce switch-telemetry", packets: [], offset: -12 },
  });
  edges.push({
    id: "kafka--dummy-producer--auction-results",
    type: "animated" as const,
    source: "kafka",
    sourceHandle: "left-out",
    target: "dummy-producer",
    targetHandle: "right-in",
    data: { label: "consume auction-results", packets: [], offset: 12 },
  });

  nodes.push({
    id: "kafka",
    type: "service" as const,
    position: { x: X_KAFKA, y: Y_BOT },
    data: { label: "Kafka", role: "kafka", status: "unknown" } as ServiceNodeData,
  });

  nodes.push({
    id: "atomix",
    type: "service" as const,
    position: { x: X_ATOMIX, y: Y_MID - 80 },
    data: { label: "Atomix", role: "atomix", status: "unknown" } as ServiceNodeData,
  });

  return { nodes, edges };
}

// ---- Packet colors ----------------------------------------------------------

const PACKET_COLORS: Record<string, string> = {
  bid: "#00e5ff",
  flow_query: "#7ee787",
  auction: "#f78166",
  auction_detail: "#f78166",
  atomix_rw: "#d2a8ff",
  telemetry: "#ffa657",
};

const PACKET_TTL_MS = 1400;

// ---- Component --------------------------------------------------------------

interface TopologyGraphProps {
  scenario: Scenario | null;
  /** Set when GET /admin/scenario failed (so we do not spin "Loading" forever). */
  scenarioError?: string | null;
  pods: PodsPayload;
  lastEvent: WSEvent<unknown> | null;
  atomixHealthy: boolean;
  kafkaHealthy: boolean;
  atomixMapNames?: string[];
  kafkaBrokers?: number;
  lastAuctionDetail?: AuctionPayload | null;
}

export function TopologyGraph({
  scenario,
  scenarioError,
  pods,
  lastEvent,
  atomixHealthy,
  kafkaHealthy,
  atomixMapNames,
  kafkaBrokers,
  lastAuctionDetail,
}: TopologyGraphProps) {
  const [nodes, setNodes, onNodesChange] = useNodesState<ServiceNodeType>([]);
  const [edges, setEdges, onEdgesChange] = useEdgesState<AnimatedEdgeType>([]);

  // Build topology once when scenario loads.
  useEffect(() => {
    if (!scenario) return;
    const { nodes: n, edges: e } = buildGraph(scenario);
    setNodes(n);
    setEdges(e);
  }, [scenario, setNodes, setEdges]);

  // Reconcile pod health into node data.
  useEffect(() => {
    setNodes((prev) =>
      prev.map((node) => {
        const nd = node.data as ServiceNodeData;
        const workloadName = workloadForNode(node.id);
        if (!workloadName) return node;
        const info = pods[workloadName];
        if (!info) return node;
        const status =
          info.desired === 0
            ? "offline"
            : info.ready === info.desired
              ? "healthy"
              : info.ready === 0
                ? "offline"
                : "degraded";
        return { ...node, data: { ...nd, podInfo: info, status } };
      })
    );
  }, [pods, setNodes]);

  // Reflect atomix / kafka connectivity and surface extra metadata.
  useEffect(() => {
    setNodes((prev) =>
      prev.map((node) => {
        const nd = node.data as ServiceNodeData;
        if (node.id === "atomix") {
          const mapNames = atomixMapNames && atomixMapNames.length > 0 ? atomixMapNames : nd.mapNames;
          return { ...node, data: { ...nd, status: atomixHealthy ? "healthy" : "degraded", mapNames, meta: undefined } };
        }
        if (node.id === "kafka") {
          const podInfo = kafkaBrokers && kafkaBrokers > 0
            ? { desired: kafkaBrokers, ready: kafkaBrokers }
            : nd.podInfo;
          return { ...node, data: { ...nd, status: kafkaHealthy ? "healthy" : "offline", podInfo, meta: undefined } };
        }
        return node;
      })
    );
  }, [atomixHealthy, kafkaHealthy, atomixMapNames, kafkaBrokers, setNodes]);

  // Show last auction time on Auction Runner node.
  useEffect(() => {
    if (!lastAuctionDetail) return;
    const t = new Date();
    const timeStr = t.toTimeString().slice(0, 8);
    setNodes((prev) =>
      prev.map((node) => {
        if (node.id !== "auction-runner") return node;
        const nd = node.data as ServiceNodeData;
        return { ...node, data: { ...nd, meta: `last auction: ${timeStr}` } };
      })
    );
  }, [lastAuctionDetail, setNodes]);

  // Fire packet animations on new events.
  useEffect(() => {
    if (!lastEvent) return;
    const { type, from, to, payload } = lastEvent;
    if (!from || !to) return;

    const label = packetLabel(type, payload);
    const color = PACKET_COLORS[type] ?? "#ffffff";

    setEdges((prev) =>
      prev.map((edge) => {
        const matchForward = edge.source === from && edge.target === to;
        const matchReverse = !!edge.data?.bidirectional && edge.source === to && edge.target === from;
        if (!matchForward && !matchReverse) return edge;

        const pkt: Packet = {
          id: `${Date.now()}-${Math.random()}`,
          edgeId: edge.id,
          label,
          color,
          createdAt: Date.now(),
          reversed: matchReverse,
        };
        const existing = (edge.data?.packets as Packet[]) ?? [];
        return { ...edge, data: { ...edge.data, packets: [...existing, pkt] } };
      })
    );

    setTimeout(() => {
      setEdges((prev) =>
        prev.map((edge) => {
          const pkts = (edge.data?.packets as Packet[]) ?? [];
          const now = Date.now();
          const active = pkts.filter((p) => now - p.createdAt < PACKET_TTL_MS + 200);
          if (active.length === pkts.length) return edge;
          return { ...edge, data: { ...edge.data, packets: active } };
        })
      );
    }, PACKET_TTL_MS + 300);
  }, [lastEvent, setEdges]);

  const onConnect = useCallback(
    (params: Connection) => setEdges((e) => addEdge(params, e)),
    [setEdges]
  );

  if (!scenario) {
    return (
      <div
        style={{
          display: "flex",
          alignItems: "center",
          justifyContent: "center",
          height: "100%",
          color: scenarioError ? "#f85149" : "#484f58",
          fontSize: 13,
          fontFamily: "'Inter', sans-serif",
          padding: 24,
          textAlign: "center",
        }}
      >
        {scenarioError ?? "Loading scenario…"}
      </div>
    );
  }

  return (
    <div style={{ width: "100%", height: "100%", background: "#0d1117" }}>
      <style>{`
        @keyframes travel {
          from { offset-distance: 0%; }
          to   { offset-distance: 100%; }
        }
        @keyframes travel-reverse {
          from { offset-distance: 100%; }
          to   { offset-distance: 0%; }
        }
      `}</style>

      <svg style={{ position: "absolute", width: 0, height: 0 }}>
        <defs>
          <marker
            id="arrowhead"
            markerWidth="6"
            markerHeight="6"
            refX="5"
            refY="3"
            orient="auto"
          >
            <path d="M0,0 L0,6 L6,3 z" fill="#30363d" />
          </marker>
        </defs>
      </svg>

      <ReactFlow
        nodes={nodes}
        edges={edges}
        onNodesChange={onNodesChange}
        onEdgesChange={onEdgesChange}
        onConnect={onConnect}
        nodeTypes={nodeTypes}
        edgeTypes={edgeTypes}
        fitView
        proOptions={{ hideAttribution: true }}
        style={{ background: "#0d1117" }}
      >
        <Background color="#21262d" gap={24} size={1} />
        <Controls
          style={{ background: "#161b22", border: "1px solid #30363d" }}
        />
        <MiniMap
          nodeColor={(n) => {
            const nd = n.data as ServiceNodeData;
            if (nd.status === "healthy") return "#3fb950";
            if (nd.status === "degraded") return "#d29922";
            if (nd.status === "offline") return "#f85149";
            return "#8b949e";
          }}
          style={{ background: "#161b22", border: "1px solid #30363d" }}
        />
      </ReactFlow>
    </div>
  );
}

// ---- helpers ----------------------------------------------------------------

function workloadForNode(nodeId: string): string {
  if (nodeId.startsWith("agent:")) {
    const cid = nodeId.slice(6);
    return `customer-agent-${cid}`;
  }
  const map: Record<string, string> = {
    "api-gateway": "api-gateway",
    "auction-runner": "auction-runner",
    "telemetry-service": "telemetry-service",
    atomix: "consensus-store",
    // dummy-producer (Switch) intentionally omitted — no pod count shown
  };
  return map[nodeId] ?? "";
}

function packetLabel(type: string, payload: unknown): string {
  if (type === "bid") {
    const p = payload as BidPayload;
    return `bid ${p.units}u@${p.unit_price}`;
  }
  if (type === "flow_query") return "GET /flows";
  if (type === "auction" || type === "auction_detail") {
    const p = payload as AuctionPayload;
    return `cp=${p.clearing_price ?? "?"}`;
  }
  if (type === "atomix_rw") {
    const p = payload as AtomixRWPayload;
    return `${p.op} ${p.map}`;
  }
  if (type === "telemetry") {
    const p = payload as TelemetryPayload;
    const keys = Object.keys(p.flows ?? {});
    return keys.length > 0 ? `flow ${keys[0]}` : "telemetry";
  }
  return type;
}
