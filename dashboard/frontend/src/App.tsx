import { useEffect, useMemo, useRef, useState } from "react";
import toast, { Toaster } from "react-hot-toast";

import { useWebSocket } from "./hooks/useWebSocket";
import { TopologyGraph } from "./components/TopologyGraph";
import { EventFeed } from "./components/EventFeed";
import { TelemetryChart } from "./components/TelemetryChart";
import { CreditsChart } from "./components/CreditsChart";
import { BidPanel } from "./components/BidPanel";
import type { TelemetryPoint } from "./components/TelemetryChart";
import type { CreditsEntry } from "./components/CreditsChart";

import type {
  Scenario,
  PodsPayload,
  FeedEntry,
  WSEvent,
  BidPayload,
  AuctionPayload,
  TelemetryPayload,
  AtomixRWPayload,
  FlowMetrics,
  CustomerCredits,
} from "./types";
import { normalizeScenario } from "./normalizeScenario";

const BASE = import.meta.env.BASE_URL;
const MAX_FEED = 120;
const MAX_CHART_POINTS = 60;

function formatTime(d: Date) {
  return d.toTimeString().slice(0, 8);
}

let feedSeq = 0;
function feedId() {
  return String(++feedSeq);
}

export default function App() {
  const [scenario, setScenario] = useState<Scenario | null>(null);
  const [scenarioError, setScenarioError] = useState<string | null>(null);
  const [pods, setPods] = useState<PodsPayload>({});
  const [feed, setFeed] = useState<FeedEntry[]>([]);
  const [telemetryHistory, setTelemetryHistory] = useState<TelemetryPoint[]>([]);
  const [flowKeys, setFlowKeys] = useState<string[]>([]);
  const [credits, setCredits] = useState<CreditsEntry[]>([]);
  const [lastEvent, setLastEvent] = useState<WSEvent<unknown> | null>(null);
  const [lastAuctionDetail, setLastAuctionDetail] = useState<AuctionPayload | null>(null);
  const lastToastedAuctionRef = useRef("");
  const [atomixHealthy, setAtomixHealthy] = useState(false);
  const [kafkaHealthy, setKafkaHealthy] = useState(false);
  const [kafkaBootstrap, setKafkaBootstrap] = useState("");
  const [kafkaBrokers, setKafkaBrokers] = useState(0);
  const [atomixMapNames, setAtomixMapNames] = useState<string[]>([]);

  // Load scenario + initial data.
  useEffect(() => {
    fetch(`${BASE}admin/scenario`)
      .then(async (r) => {
        if (!r.ok) {
          const body = await r.text();
          console.warn("scenario fetch failed:", r.status, body);
          setScenarioError(`Could not load scenario (HTTP ${r.status}).`);
          return null;
        }
        setScenarioError(null);
        return normalizeScenario(await r.json());
      })
      .then(setScenario)
      .catch((e) => {
        console.error(e);
        setScenarioError("Could not load scenario.");
        setScenario(null);
      });
    refreshCredits();
  }, []);

  // Poll health endpoint every 10 s.
  useEffect(() => {
    function poll() {
      fetch(`${BASE}admin/health`)
        .then((r) => r.json())
        .then((h: {
          atomix: boolean;
          kafka: boolean;
          kafka_bootstrap?: string;
          kafka_brokers?: number;
          atomix_maps?: string[];
        }) => {
          setAtomixHealthy(h.atomix);
          setKafkaHealthy(h.kafka);
          if (h.kafka_bootstrap) setKafkaBootstrap(h.kafka_bootstrap);
          if (h.kafka_brokers != null) setKafkaBrokers(h.kafka_brokers);
          if (h.atomix_maps) setAtomixMapNames(h.atomix_maps);
        })
        .catch(() => {
          setAtomixHealthy(false);
        });
    }
    poll();
    const id = setInterval(poll, 10_000);
    return () => clearInterval(id);
  }, []);

  function refreshCredits() {
    fetch(`${BASE}admin/credits`)
      .then(async (r) => {
        if (!r.ok) return null;
        return r.json() as Promise<{
          credits?: Record<string, CustomerCredits>;
          utility?: Record<string, number>;
        }>;
      })
      .then((data) => {
        if (!data?.credits) return;
        const utility = data.utility ?? {};
        const entries: CreditsEntry[] = Object.entries(data.credits)
          .map(([id, c]) => ({
            customer: id,
            balance: c.starting_balance - c.total_spent,
            spent: c.total_spent,
            utility: utility[id] ?? 0,
          }))
          .filter((e) => e.spent > 0 || e.utility > 0);
        setCredits(entries);
      })
      .catch(console.error);
  }

  function pushFeed(entry: Omit<FeedEntry, "id">) {
    setFeed((prev) => [{ ...entry, id: feedId() }, ...prev].slice(0, MAX_FEED));
  }

  // WebSocket handlers.
  useWebSocket({
    bid: (ev) => {
      const p = ev.payload as BidPayload;
      setLastEvent(ev as WSEvent<unknown>);
      pushFeed({
        time: formatTime(new Date()),
        icon: "💸",
        text: `${p.customer_id} → egress ${p.egress_port}: ${p.units} kbps @ ${p.unit_price}/unit`,
        color: "#00e5ff",
        category: "bid",
      });
    },

    flow_query: (ev) => {
      setLastEvent(ev as WSEvent<unknown>);
      // Intentionally not added to feed — topology animation is sufficient.
    },

    auction: (ev) => {
      setLastEvent(ev as WSEvent<unknown>);
    },

    auction_detail: (ev) => {
      const p = ev.payload as AuctionPayload;
      setLastEvent(ev as WSEvent<unknown>);
      setLastAuctionDetail(p);

      // Deduplicate: auction runner emits one message per allocation, all for the same egress+interval.
      const toastKey = `${p.egress_port}-${p.interval || p.clearing_price}`;
      if (lastToastedAuctionRef.current === toastKey) return;
      lastToastedAuctionRef.current = toastKey;

      const allocLines = (p.allocations ?? []).map(
        (a, i, arr) =>
          `${i === arr.length - 1 ? "└" : "├"} ingress ${String(a.ingress_port).padEnd(6)} ${a.units} kbps`
      );

      toast(
        <div
          style={{
            fontFamily: "'JetBrains Mono', monospace",
            fontSize: 12,
            lineHeight: 1.6,
          }}
        >
          <div style={{ fontWeight: 700, marginBottom: 6, color: "#e6edf3" }}>
            Egress Port {p.egress_port} — Auction Result
          </div>
          <div style={{ color: "#8b949e", marginBottom: 4 }}>
            Clearing price: <span style={{ color: "#f78166" }}>{p.clearing_price}</span> /unit
          </div>
          {allocLines.map((line, i) => (
            <div key={i} style={{ color: "#e6edf3", whiteSpace: "pre" }}>
              {line}
            </div>
          ))}
        </div>,
        {
          duration: 7000,
          style: {
            background: "#161b22",
            border: "1px solid #f78166",
            borderRadius: 8,
            padding: "12px 16px",
            maxWidth: 520,
          },
          icon: null,
        }
      );

      const feedAllocLines = (p.allocations ?? []).map(
        (a, i, arr) =>
          `${i === arr.length - 1 ? "└" : "├"} ingress ${String(a.ingress_port).padEnd(6)} ${a.units} kbps`
      );
      const feedHeader = `Egress ${p.egress_port} cleared @ ${p.clearing_price}/unit`;
      const feedText = feedAllocLines.length > 0
        ? `${feedHeader}\n${feedAllocLines.join("\n")}`
        : feedHeader;

      pushFeed({
        time: formatTime(new Date()),
        icon: "🔨",
        text: feedText,
        color: "#f78166",
        category: "auction",
      });

      // Refresh credits after auction settles.
      setTimeout(refreshCredits, 1000);
    },

    atomix_rw: (ev) => {
      const p = ev.payload as AtomixRWPayload;
      setLastEvent(ev as WSEvent<unknown>);
      pushFeed({
        time: formatTime(new Date()),
        icon: "💾",
        text: `${ev.from} ${p.op} ${p.map}`,
        color: "#d2a8ff",
        category: "atomix",
      });
    },

    telemetry: (ev) => {
      const p = ev.payload as TelemetryPayload;
      setLastEvent(ev as WSEvent<unknown>);

      const keys = Object.keys(p.flows ?? {});
      if (keys.length === 0) return;

      setFlowKeys((prev) => {
        const merged = Array.from(new Set([...prev, ...keys]));
        return merged;
      });

      const point: TelemetryPoint = { time: formatTime(new Date()) };
      keys.forEach((k) => {
        const m = p.flows[k] as FlowMetrics;
        point[`${k} in`] = Math.round(m.throughput_kbps);
        point[`${k} eg`] = Math.round(m.egress_kbps);
        point[`${k} drop%`] = Math.round(m.drop_rate_pct * 10) / 10;
      });

      setTelemetryHistory((prev) =>
        [...prev, point].slice(-MAX_CHART_POINTS)
      );
    },

    pods: (ev) => {
      const p = ev.payload as PodsPayload;
      setPods(p);
    },
  });

  // Only show flow keys with non-zero values in the latest data point.
  const activeChartKeys = useMemo(() => {
    if (telemetryHistory.length === 0) return flowKeys.flatMap((k) => [`${k} in`, `${k} eg`]);
    const latest = telemetryHistory[telemetryHistory.length - 1];
    const activeBase = flowKeys.filter((k) => {
      const inVal = latest[`${k} in`];
      const egVal = latest[`${k} eg`];
      return (typeof inVal === "number" && inVal > 0) || (typeof egVal === "number" && egVal > 0);
    });
    return activeBase.flatMap((k) => [`${k} in`, `${k} eg`]);
  }, [telemetryHistory, flowKeys]);

  return (
    <div
      style={{
        display: "grid",
        gridTemplateRows: "auto 1fr auto",
        gridTemplateColumns: "1fr",
        height: "100vh",
        background: "#0d1117",
        color: "#e6edf3",
        fontFamily: "'Inter', sans-serif",
        overflow: "hidden",
      }}
    >
      <Toaster position="top-right" />

      {/* Header */}
      <header
        style={{
          padding: "10px 20px",
          borderBottom: "1px solid #30363d",
          display: "flex",
          alignItems: "center",
          gap: 12,
          background: "#161b22",
        }}
      >
        <span style={{ fontSize: 18 }}>⚡</span>
        <span
          style={{
            fontWeight: 700,
            fontSize: 14,
            letterSpacing: "0.02em",
            color: "#e6edf3",
          }}
        >
          IXP Control Plane Monitor
        </span>
        {scenario && (
          <span style={{ color: "#8b949e", fontSize: 12, marginLeft: 8 }}>
            {scenario.name} · auction every {scenario.auction_interval}
          </span>
        )}
        <div style={{ marginLeft: "auto", display: "flex", gap: 12 }}>
          <StatusPill
            label="Atomix"
            ok={atomixHealthy}
            detail={atomixMapNames.length > 0 ? `${atomixMapNames.length} maps` : undefined}
          />
          <StatusPill
            label="Kafka"
            ok={kafkaHealthy}
            detail={kafkaBootstrap || undefined}
          />
        </div>
      </header>

      {/* Main grid */}
      <div
        style={{
          display: "grid",
          gridTemplateColumns: "1fr minmax(320px, 35vw)",
          gridTemplateRows: "1fr",
          overflow: "hidden",
          minHeight: 0,
        }}
      >
        {/* Topology canvas */}
        <div style={{ borderRight: "1px solid #30363d", overflow: "hidden" }}>
          <TopologyGraph
            scenario={scenario}
            scenarioError={scenarioError}
            pods={pods}
            lastEvent={lastEvent}
            atomixHealthy={atomixHealthy}
            kafkaHealthy={kafkaHealthy}
            atomixMapNames={atomixMapNames}
            kafkaBrokers={kafkaBrokers}
            lastAuctionDetail={lastAuctionDetail}
          />
        </div>

        {/* Right sidebar: event feed */}
        <div
          style={{
            background: "#0d1117",
            overflow: "hidden",
            borderLeft: "1px solid #30363d",
          }}
        >
          <EventFeed entries={feed} />
        </div>
      </div>

      {/* Bottom panels */}
      <div
        style={{
          display: "grid",
          gridTemplateColumns: "1fr 1fr minmax(320px, 380px)",
          height: 220,
          borderTop: "1px solid #30363d",
          background: "#161b22",
        }}
      >
        <div style={{ borderRight: "1px solid #30363d", overflow: "hidden" }}>
          <TelemetryChart data={telemetryHistory} flowKeys={activeChartKeys} />
        </div>
        <div style={{ borderRight: "1px solid #30363d", overflow: "hidden" }}>
          <CreditsChart data={credits} />
        </div>
        <div style={{ overflow: "hidden" }}>
          <BidPanel scenario={scenario} />
        </div>
      </div>
    </div>
  );
}

// ---- StatusPill -------------------------------------------------------------

function StatusPill({ label, ok, detail }: { label: string; ok: boolean; detail?: string }) {
  return (
    <span
      style={{
        display: "inline-flex",
        alignItems: "center",
        gap: 5,
        background: "#0d1117",
        border: `1px solid ${ok ? "#3fb950" : "#f85149"}`,
        borderRadius: 20,
        padding: "2px 10px",
        fontSize: 10,
        fontFamily: "'JetBrains Mono', monospace",
        color: ok ? "#3fb950" : "#f85149",
      }}
    >
      <span style={{ fontSize: 8 }}>●</span>
      {label}
      {detail && (
        <span style={{ color: "#8b949e", marginLeft: 2 }}>{detail}</span>
      )}
    </span>
  );
}
