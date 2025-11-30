# This code tests the mock local controller

# Start the mock local controller, kafka (redpanda), and the mock callback server
docker compose up -d

# Call StartTelemetry on the local controller
grpcurl -plaintext -proto proto/controller.proto \
  -d '{"kafka_broker_addr":"redpanda:9092", "topic": "test-telemetry"}' \
  localhost:50051 \
  localcontroller.LocalController/StartTelemetry

# Call StartCallback on the local controller
grpcurl -plaintext -proto proto/controller.proto \
  -d '{"server_addr":"http://callback:8080/callback"}' \
  localhost:50051 \
  localcontroller.LocalController/StartCallback

# Call configure on local controller, this makes it start sending callbacks
grpcurl -plaintext -proto proto/controller.proto \
  -d '{"msg":"test"}' \
  localhost:50051 localcontroller.LocalController/Configure

# Wait for a while to let telemetry and callback to run a few times
sleep 10s

# Verify logs
docker compose logs localcontroller
docker compose logs callback

# Check telemetry entries
docker compose exec -it redpanda rpk topic consume test-telemetry