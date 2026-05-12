# System Architecture Diagrams

## 1. Auction System - Resource Bidding Pipeline
```mermaid
graph LR
    Agent["👤 Customer Agents"] 
    API["API Gateway"]
    Atomix["Atomix<br/>Distributed Store"]
    Auction["Auction Runner"]
    Kafka["Apache Kafka"]
    LocalCP["Local Control Plane<br/>(out of scope)"]
    Switches["Data Plane<br/>Switches"]
    
    Agent -->|1. Submit bids<br/>HTTP POST /bid| API
    API -->|2. Store bid<br/>+ trace context| Atomix
    API -->|4. Return response| Agent
    
    Auction -->|3. Read bids<br/>on timer| Atomix
    Auction -->|5. Execute auction<br/>algorithm| Auction
    Auction -->|6. Publish clearing<br/>results| Kafka
    
    Kafka -->|7. Consume<br/>allocation| LocalCP
    LocalCP -->|8. Enforce<br/>policies| Switches
    
    style Agent fill:#e1f5ff
    style API fill:#fff3e0
    style Atomix fill:#f3e5f5
    style Auction fill:#fff3e0
    style Kafka fill:#e8f5e9
    style LocalCP fill:#fce4ec
    style Switches fill:#fce4ec
```

## 2. Telemetry System - Flow Metrics Collection Pipeline
```mermaid
graph LR
    Switches["Data Plane<br/>Switches"]
    Kafka["Apache Kafka<br/>Flow Statistics Topic"]
    Telemetry["Telemetry<br/>Processor"]
    Atomix["Atomix<br/>Distributed Store"]
    Agent["👤 Customer Agents"]
    API["API Gateway"]
    
    Switches -->|1. Periodically send<br/>flow statistics| Kafka
    Kafka -->|2. Consume<br/>flow metrics| Telemetry
    Telemetry -->|3. Process &<br/>aggregate| Telemetry
    Telemetry -->|4. Store in<br/>distributed storage| Atomix
    
    Agent -->|6. Query available<br/>metrics| API
    API -->|7. Read flow<br/>state| Atomix
    API -->|8. Return metrics<br/>for bidding| Agent
    
    style Switches fill:#fce4ec
    style Kafka fill:#e8f5e9
    style Telemetry fill:#fff3e0
    style Atomix fill:#f3e5f5
    style Agent fill:#e1f5ff
    style API fill:#fff3e0
```

## 3. Integrated System - Auction + Telemetry + Observability
```mermaid
graph TB
    subgraph "Control Plane Services"
        API["API Gateway<br/>HTTP Interface"]
        Auction["Auction Runner<br/>Clearing Logic"]
        Telemetry["Telemetry Processor<br/>Metrics Pipeline"]
    end
    
    subgraph "Shared Infrastructure"
        Atomix["Atomix<br/>Distributed Consensus Store<br/>Raft Protocol"]
        Kafka["Apache Kafka<br/>Asynchronous Message Broker"]
    end
    
    subgraph "Observability Stack"
        Collector["OpenTelemetry Collector<br/>Tail Sampling"]
        Prometheus["Prometheus<br/>Metrics"]
        Jaeger["Jaeger<br/>Distributed Traces"]
        Loki["Loki<br/>Log Aggregation"]
    end
    
    subgraph "External"
        Agent["Customer Agents"]
        Switches["Data Plane Switches"]
    end
    
    Agent -->|Bids + Query| API
    API -->|Store/Read| Atomix
    Auction -->|Read Bids| Atomix
    Auction -->|Publish Results| Kafka
    Switches -->|Flow Stats| Kafka
    Telemetry -->|Consume| Kafka
    Telemetry -->|Store Metrics| Atomix
    
    API -->|Emit OTLP| Collector
    Auction -->|Emit OTLP| Collector
    Telemetry -->|Emit OTLP| Collector
    
    Collector -->|Metrics| Prometheus
    Collector -->|Traces| Jaeger
    Collector -->|Logs| Loki
    
    style API fill:#fff3e0
    style Auction fill:#fff3e0
    style Telemetry fill:#fff3e0
    style Atomix fill:#f3e5f5
    style Kafka fill:#e8f5e9
    style Collector fill:#c8e6c9
    style Prometheus fill:#c8e6c9
    style Jaeger fill:#c8e6c9
    style Loki fill:#c8e6c9
```

## 4. Atomix Data Model - Shared State Structure
```mermaid
graph LR
    Atomix["Atomix Distributed Store"]
    
    subgraph "Bid Storage"
        Bids["Map: 'bids'<br/>Key: bid_id<br/>Value: Bid Object<br/>+ W3C TraceContext"]
    end
    
    subgraph "Flow Metrics"
        Flows["Map: 'flows'<br/>Key: flow_id<br/>Value: FlowMetrics<br/>Throughput, utilization"]
    end
    
    subgraph "Auction State"
        Auctions["Map: 'auctions'<br/>Key: interval_id<br/>Value: AuctionResult<br/>Clearing price, allocations"]
    end
    
    Atomix --> Bids
    Atomix --> Flows
    Atomix --> Auctions
    
    style Atomix fill:#f3e5f5
    style Bids fill:#e1bee7
    style Flows fill:#e1bee7
    style Auctions fill:#e1bee7
```

## 5. Kafka Message Queue Architecture
```mermaid
graph TB
    subgraph "Producers"
        DummySwitch["DummyProducer<br/>(Simulates Data Plane)"]
        AuctionRunner["Auction Runner<br/>(Control Plane)"]
    end
    
    subgraph "Apache Kafka Broker"
        Topic1["Topic: switch-telemetry<br/>─────────────────<br/>Message: TelemetryRecord<br/>{flow_id, rx_bytes, tx_bytes, drop_bytes}<br/>Retention: 7 days<br/>Partition: 1 (test env)"]
        Topic2["Topic: auction-results<br/>─────────────────<br/>Message: AuctionResultRecord<br/>{ingress_port, egress_port, bandwidth_kbps}<br/>Retention: 30 days<br/>Partition: 1 (test env)"]
    end
    
    subgraph "Consumers"
        TelemetryProc["Telemetry Processor<br/>(Aggregates flows)"]
        DummySwitchCons["DummySwitch<br/>(Applies allocations)"]
    end
    
    DummySwitch -->|Produces every 1s<br/>Flow statistics| Topic1
    AuctionRunner -->|Produces every 30s<br/>Clearing results| Topic2
    
    Topic1 -->|Consumes<br/>Flow metrics| TelemetryProc
    Topic2 -->|Consumes<br/>Allocations| DummySwitchCons
    
    TelemetryProc -->|Aggregates &<br/>stores in Atomix| Atomix["Atomix<br/>Distributed Store"]
    
    style DummySwitch fill:#fce4ec
    style AuctionRunner fill:#fff3e0
    style Topic1 fill:#e8f5e9
    style Topic2 fill:#e8f5e9
    style TelemetryProc fill:#fff3e0
    style DummySwitchCons fill:#fce4ec
    style Atomix fill:#f3e5f5
```
