// ---- Scenario types (mirrors shared/scenario/types.go) ----------------------

export interface ScenarioSwitch {
  id: string;
  ingress_ports: number[];
  egress_ports: number[];
  max_capacity: number;
}

export interface ScenarioCustomer {
  id: string;
  switch_id: string;
  ingress_ports: number[];
  strategy: string;
  starting_balance: number;
  valuation_per_unit: number;
}

export interface Scenario {
  name: string;
  switches: ScenarioSwitch[];
  customers: ScenarioCustomer[];
  auction_interval: string;
  telemetry_interval: string;
  reservation_price: number;
  auction_result_kafka_topic: string;
  telemetry_kafka_topic: string;
}

// ---- Flow metrics -----------------------------------------------------------

export interface FlowMetrics {
  throughput_kbps: number;
  egress_kbps: number;
  drop_kbps: number;
  drop_rate_pct: number;
}

// ---- Auction ----------------------------------------------------------------

export interface AuctionAllocation {
  customer_id: string;
  ingress_port: number;
  units: number;
}

export interface AuctionRecord {
  interval: string;
  egress_port: number;
  clearing_price: number;
  allocations: AuctionAllocation[];
}

// ---- Credits & utility -------------------------------------------------------

export interface CustomerCredits {
  total_spent: number;
  starting_balance: number;
}

// ---- Pod info ----------------------------------------------------------------

export interface PodInfo {
  desired: number;
  ready: number;
}

export type PodsPayload = Record<string, PodInfo>;

// ---- WebSocket event envelopes ----------------------------------------------

export type EventType =
  | "bid"
  | "flow_query"
  | "auction"
  | "auction_detail"
  | "atomix_rw"
  | "telemetry"
  | "pods";

export interface WSEvent<P = unknown> {
  type: EventType;
  from?: string;
  to?: string;
  payload: P;
}

export interface BidPayload {
  customer_id: string;
  egress_port: number;
  ingress_port: string;
  units: number;
  unit_price: number;
  timestamp: string;
}

export interface AuctionPayload extends AuctionRecord {
  timestamp: string;
}

export interface TelemetryPayload {
  flows: Record<string, FlowMetrics>;
}

export interface AtomixRWPayload {
  op: string;
  map: string;
}

// ---- Topology node data -------------------------------------------------------
// Must extend Record<string, unknown> for React Flow v12 compatibility.

export interface ServiceNodeData extends Record<string, unknown> {
  label: string;
  /** "api-gateway" | "auction-runner" | "telemetry-service" | "atomix" | "kafka" | "agent" | "dummy" */
  role: string;
  podInfo?: PodInfo;
  status?: "healthy" | "degraded" | "offline" | "unknown";
  meta?: string; // shown below pod count (gray)
  mapNames?: string[]; // shown on hover as a tooltip list
  customerId?: string; // for agent nodes
}

// ---- Feed entry -------------------------------------------------------------

export interface FeedEntry {
  id: string;
  time: string;
  icon: string;
  text: string;
  color: string;
  category?: "auction" | "bid" | "atomix";
}

// ---- Animated packet --------------------------------------------------------

export interface Packet {
  id: string;
  edgeId: string;
  label: string;
  color: string;
  createdAt: number;
  /** When true the packet travels from target→source (reverse along a bidirectional edge). */
  reversed?: boolean;
}
