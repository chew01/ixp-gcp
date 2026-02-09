package scenario

type Scenario struct {
	Version                 string   `yaml:"version"`
	Name                    string   `yaml:"name"`
	Switches                []Switch `yaml:"switches"`
	AuctionInterval         string   `yaml:"auction_interval"`
	ReservationPrice        int      `yaml:"reservation_price"`
	AuctionResultKafkaTopic string   `yaml:"auction_result_kafka_topic"`
	TelemetryKafkaTopic     string   `yaml:"telemetry_kafka_topic"`
}

type Switch struct {
	ID           string   `yaml:"id"`
	IngressPorts []uint64 `yaml:"ingress_ports"`
	EgressPorts  []uint64 `yaml:"egress_ports"`
	MaxCapacity  uint64   `yaml:"max_capacity"` // assuming all egress ports are similar bandwidth
}
