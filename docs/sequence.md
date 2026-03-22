# Sequence Diagrams

### Auction System
```mermaid
sequenceDiagram
    autonumber
    actor C as Customer Agent
    participant A as API Service
    participant D as Atomix
    participant R as Auction Runner
    participant K as Kafka
    participant S as Switch

    C ->> A: POST /bids (X-Customer-ID, ingress, egress, units, unit_price)
    A ->> D: BidMap.Put(units|price|customer_id)
    Note over A: One bid per (ingress, egress)<br/>value encodes customer ID

    R ->> D: BidMap.List()
    D -->> R: bids with customer IDs
    Note over R: Run auction<br/>compute allocations + clearing price

    R ->> D: CreditsMap.Update(total_spent per customer)
    R ->> D: AuctionHistoryMap.Put(interval, egress, clearing_price, per-customer allocations)

    R ->> K: Auction result
    S ->> K: Consume result
    K -->> S: Auction result
    Note over S: Configure switch

    C ->> A: GET /credits (X-Customer-ID)
    A -->> C: total_spent for that customer

    C ->> A: GET /auctions?egress_port=0 (X-Customer-ID)
    A -->> C: clearing prices + caller's own allocations
```

### Telemetry System
```mermaid
sequenceDiagram
    autonumber
    actor U as User
    participant G as Graf + Prom
    participant A as API Service
    participant D as Atomix
    participant T as Telemetry Processor
    participant K as Kafka
    participant S as Switch

    S ->> K: Sends telemetry
    T ->> K: Consume telemetry
    K -->> T: Telemetry data
    Note over T: Calculate throughput
    T ->> D: ThroughputMap.Set()
    G ->> A: GET /metrics
    A ->> D: ThroughputMap.List()
    D -->> A: All throughput data
    A -->> G: All metrics
    U ->> G: Access Grafana
    G -->> U: Visual data
    U ->> A: GET /flows
    A -->> U: Real time throughput data
```