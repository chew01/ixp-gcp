package scenario

type Scenario struct {
	Version                 string     `yaml:"version"`
	Name                    string     `yaml:"name"`
	Switches                []Switch   `yaml:"switches"`
	Customers               []Customer `yaml:"customers"`
	TelemetryInterval       string     `yaml:"telemetry_interval"`
	AuctionInterval         string     `yaml:"auction_interval"`
	ReservationPrice        int        `yaml:"reservation_price"`
	AuctionResultKafkaTopic string     `yaml:"auction_result_kafka_topic"`
	TelemetryKafkaTopic     string     `yaml:"telemetry_kafka_topic"`
}

// Customer defines an AS provider that owns ingress ports on a switch.
// The same customer ID may appear in multiple entries if they own ports on multiple switches.
type Customer struct {
	ID           string   `yaml:"id"`            // e.g. "as12345"
	SwitchID     string   `yaml:"switch_id"`     // switch these ports belong to
	IngressPorts []uint32 `yaml:"ingress_ports"` // ports this customer owns on the switch
}

type Switch struct {
	ID           string   `yaml:"id"`
	IngressPorts []uint32 `yaml:"ingress_ports"`
	EgressPorts  []uint32 `yaml:"egress_ports"`
	MaxCapacity  uint64   `yaml:"max_capacity"`
}
