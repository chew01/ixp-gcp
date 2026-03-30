import type { Scenario, ScenarioCustomer, ScenarioSwitch } from "./types";

function toArray<T>(v: unknown): T[] {
  if (Array.isArray(v)) return v as T[];
  return [];
}

/**
 * Accepts JSON from GET /admin/scenario. Handles snake_case keys, legacy
 * Go default names (Customers, Switches), and null slices (JSON null).
 */
export function normalizeScenario(raw: unknown): Scenario | null {
  if (!raw || typeof raw !== "object") return null;
  const o = raw as Record<string, unknown>;

  return {
    name: String(o.name ?? o.Name ?? ""),
    switches: toArray<ScenarioSwitch>(o.switches ?? o.Switches),
    customers: toArray<ScenarioCustomer>(o.customers ?? o.Customers),
    auction_interval: String(o.auction_interval ?? o.AuctionInterval ?? ""),
    telemetry_interval: String(o.telemetry_interval ?? o.TelemetryInterval ?? ""),
    reservation_price: Number(o.reservation_price ?? o.ReservationPrice ?? 0),
    auction_result_kafka_topic: String(
      o.auction_result_kafka_topic ?? o.AuctionResultKafkaTopic ?? ""
    ),
    telemetry_kafka_topic: String(
      o.telemetry_kafka_topic ?? o.TelemetryKafkaTopic ?? ""
    ),
  };
}
