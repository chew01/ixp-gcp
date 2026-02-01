from kafka import KafkaProducer
import json
import time
import random

producer = KafkaProducer(
    bootstrap_servers=["ixp-kafka-kafka-bootstrap:9092"],
    value_serializer=lambda v: json.dumps(v).encode("utf-8"),
    key_serializer=lambda k: k.encode("utf-8")
)

TOPIC = "switch-traffic-digests"
SWITCH_ID = "sw-1"

# configurable
FLOWS_PER_WINDOW = 5
WINDOW_SEC = 1

while True:
    window_start_ns = time.time_ns()
    time.sleep(WINDOW_SEC)
    window_end_ns = time.time_ns()

    flows = []

    for _ in range(FLOWS_PER_WINDOW):
        ingress = random.randint(1, 4)
        egress = random.randint(5, 8)

        flows.append({
            "ingress_port": ingress,
            "egress_port": egress,
            "bytes": random.randint(500_000, 2_000_000)
        })

    record = {
        "schema_version": 1,
        "switch_id": SWITCH_ID,
        "window_start_ns": window_start_ns,
        "window_end_ns": window_end_ns,
        "flows": flows
    }

    key = f"{SWITCH_ID}|{window_start_ns}"

    producer.send(TOPIC, key=key, value=record)
    producer.flush()

    print(f"Sent window {window_start_ns} with {len(flows)} flows")

