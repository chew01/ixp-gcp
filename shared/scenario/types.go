package scenario

type Scenario struct {
	Version                 string         `yaml:"version"`
	Name                    string         `yaml:"name"`
	Switches                []Switch       `yaml:"switches"`
	Parameters              map[string]int `yaml:"parameters"`
	AuctionResultKafkaTopic string         `yaml:"auction_result_kafka_topic"`
	TelemetryKafkaTopic     string         `yaml:"telemetry_kafka_topic"`
}

type Switch struct {
	ID           string `yaml:"id"`
	IngressPorts []int  `yaml:"ingress_ports"`
	EgressPorts  []int  `yaml:"egress_ports"`
	MaxCapacity  uint64 `yaml:"max_capacity"` // assuming all egress ports are similar bandwidth
}
