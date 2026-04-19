import { useState, type FormEvent } from "react";
import type { Scenario } from "../types";

const BASE = import.meta.env.BASE_URL;

const inputStyle: React.CSSProperties = {
  background: "#0d1117",
  border: "1px solid #30363d",
  borderRadius: 4,
  color: "#e6edf3",
  fontFamily: "'JetBrains Mono', monospace",
  fontSize: 11,
  padding: "3px 7px",
  width: "100%",
  boxSizing: "border-box",
};

const labelStyle: React.CSSProperties = {
  color: "#8b949e",
  fontSize: 10,
  fontFamily: "'JetBrains Mono', monospace",
  marginBottom: 2,
  display: "block",
};

interface BidPanelProps {
  scenario: Scenario | null;
}

export function BidPanel({ scenario }: BidPanelProps) {
  const [customerId, setCustomerId] = useState("");
  const [ingressPort, setIngressPort] = useState("");
  const [egressPort, setEgressPort] = useState("");
  const [units, setUnits] = useState("");
  const [unitPrice, setUnitPrice] = useState("");
  const [status, setStatus] = useState<{ ok: boolean; msg: string } | null>(null);
  const [loading, setLoading] = useState(false);

  const customers = scenario?.customers ?? [];
  const selectedCustomer = customers.find((c) => c.id === customerId);
  const ingressPorts = selectedCustomer?.ingress_ports ?? [];
  const egressPorts = Array.from(
    new Set((scenario?.switches ?? []).flatMap((sw) => sw.egress_ports))
  ).sort((a, b) => a - b);

  function handleCustomerChange(id: string) {
    setCustomerId(id);
    setIngressPort("");
  }

  async function handleSubmit(e: FormEvent) {
    e.preventDefault();
    setLoading(true);
    setStatus(null);
    try {
      const res = await fetch(`${BASE}admin/bid`, {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({
          customer_id: customerId,
          ingress_port: Number(ingressPort),
          egress_port: Number(egressPort),
          units: Number(units),
          unit_price: Number(unitPrice),
        }),
      });
      const text = await res.text();
      setStatus({ ok: res.ok, msg: res.ok ? "Bid accepted" : text.trim() });
    } catch (err) {
      setStatus({ ok: false, msg: String(err) });
    } finally {
      setLoading(false);
    }
  }

  return (
    <div
      style={{
        display: "flex",
        flexDirection: "column",
        height: "100%",
        padding: "10px 14px",
        boxSizing: "border-box",
        fontFamily: "'JetBrains Mono', monospace",
        overflow: "hidden",
      }}
    >
      <div
        style={{
          fontSize: 11,
          fontWeight: 700,
          color: "#8b949e",
          letterSpacing: "0.06em",
          textTransform: "uppercase",
          marginBottom: 8,
          flexShrink: 0,
        }}
      >
        Manual Bid
      </div>

      <form
        onSubmit={handleSubmit}
        style={{ display: "flex", flexDirection: "column", gap: 6, flex: 1, minHeight: 0 }}
      >
        {/* Row 1: Customer + Ingress */}
        <div style={{ display: "grid", gridTemplateColumns: "1fr 1fr", gap: 6 }}>
          <div>
            <label style={labelStyle}>Customer</label>
            <select
              value={customerId}
              onChange={(e) => handleCustomerChange(e.target.value)}
              required
              style={{ ...inputStyle, cursor: "pointer" }}
            >
              <option value="">— select —</option>
              {customers.map((c) => (
                <option key={c.id} value={c.id}>
                  {c.id}
                </option>
              ))}
            </select>
          </div>
          <div>
            <label style={labelStyle}>Ingress port</label>
            <select
              value={ingressPort}
              onChange={(e) => setIngressPort(e.target.value)}
              required
              disabled={ingressPorts.length === 0}
              style={{ ...inputStyle, cursor: "pointer" }}
            >
              <option value="">— select —</option>
              {ingressPorts.map((p) => (
                <option key={p} value={p}>
                  {p}
                </option>
              ))}
            </select>
          </div>
        </div>

        {/* Row 2: Egress + Units + Unit price */}
        <div style={{ display: "grid", gridTemplateColumns: "1fr 1fr 1fr", gap: 6 }}>
          <div>
            <label style={labelStyle}>Egress port</label>
            <select
              value={egressPort}
              onChange={(e) => setEgressPort(e.target.value)}
              required
              style={{ ...inputStyle, cursor: "pointer" }}
            >
              <option value="">— select —</option>
              {egressPorts.map((p) => (
                <option key={p} value={p}>
                  {p}
                </option>
              ))}
            </select>
          </div>
          <div>
            <label style={labelStyle}>Units (kbps)</label>
            <input
              type="number"
              min={1}
              value={units}
              onChange={(e) => setUnits(e.target.value)}
              required
              placeholder="e.g. 500"
              style={inputStyle}
            />
          </div>
          <div>
            <label style={labelStyle}>Unit price</label>
            <input
              type="number"
              min={1}
              value={unitPrice}
              onChange={(e) => setUnitPrice(e.target.value)}
              required
              placeholder="e.g. 10"
              style={inputStyle}
            />
          </div>
        </div>

        {/* Submit + status */}
        <div style={{ display: "flex", alignItems: "center", gap: 10, marginTop: 2 }}>
          <button
            type="submit"
            disabled={loading}
            style={{
              background: loading ? "#21262d" : "#1f6feb",
              border: "1px solid #388bfd",
              borderRadius: 4,
              color: loading ? "#8b949e" : "#e6edf3",
              fontFamily: "'JetBrains Mono', monospace",
              fontSize: 11,
              fontWeight: 700,
              padding: "4px 16px",
              cursor: loading ? "default" : "pointer",
              flexShrink: 0,
            }}
          >
            {loading ? "Sending…" : "Submit bid"}
          </button>
          {status && (
            <span
              style={{
                fontSize: 11,
                color: status.ok ? "#3fb950" : "#f85149",
                overflow: "hidden",
                textOverflow: "ellipsis",
                whiteSpace: "nowrap",
              }}
            >
              {status.msg}
            </span>
          )}
        </div>
      </form>
    </div>
  );
}
