package scenario

type Scenario struct {
	Version                 string        `yaml:"version"`
	Name                    string        `yaml:"name"`
	Switches                []Switch      `yaml:"switches"`
	Customers               []Customer    `yaml:"customers"`
	TelemetryInterval       string        `yaml:"telemetry_interval"`
	AuctionInterval         string        `yaml:"auction_interval"`
	ReservationPrice        int           `yaml:"reservation_price"`
	AuctionResultKafkaTopic string        `yaml:"auction_result_kafka_topic"`
	TelemetryKafkaTopic     string        `yaml:"telemetry_kafka_topic"`
	Traffic                 TrafficConfig `yaml:"traffic"`
}

// Customer defines an AS provider that owns ingress ports on a switch.
// The same customer ID may appear in multiple entries if they own ports on multiple switches.
type Customer struct {
	ID             string            `yaml:"id"`              // e.g. "as12345"
	SwitchID       string            `yaml:"switch_id"`       // switch these ports belong to
	IngressPorts   []uint32          `yaml:"ingress_ports"`   // ports this customer owns on the switch
	Strategy       string            `yaml:"strategy"`        // bidding strategy name (default: "conservative")
	StrategyParams map[string]string `yaml:"strategy_params"` // strategy-specific tuning parameters
}

type Switch struct {
	ID           string   `yaml:"id"`
	IngressPorts []uint32 `yaml:"ingress_ports"`
	EgressPorts  []uint32 `yaml:"egress_ports"`
	MaxCapacity  uint64   `yaml:"max_capacity"`
}

// Traffic pattern names for TrafficConfig.Pattern.
const (
	TrafficPatternRandom = "random"
	TrafficPatternSteady = "steady"
	TrafficPatternSpike  = "spike"
)

// TrafficConfig controls how the dummy switch producer generates traffic.
// Zero values fall back to the defaults applied by WithDefaults.
type TrafficConfig struct {
	Pattern             string `yaml:"pattern"`               // "random" | "steady" | "spike" (default: "random")
	RateKbps            int    `yaml:"rate_kbps"`             // target kbps per flow for steady/spike (default: 10)
	SpikeAfterIntervals int    `yaml:"spike_after_intervals"` // intervals before spike triggers (default: 5)
	SpikeRateKbps       int    `yaml:"spike_rate_kbps"`       // kbps per flow after spike (default: 18)
}

// WithDefaults returns a copy of t with zero/empty fields replaced by defaults.
func (t TrafficConfig) WithDefaults() TrafficConfig {
	if t.Pattern == "" {
		t.Pattern = TrafficPatternRandom
	}
	if t.RateKbps <= 0 {
		t.RateKbps = 10
	}
	if t.SpikeAfterIntervals <= 0 {
		t.SpikeAfterIntervals = 5
	}
	if t.SpikeRateKbps <= 0 {
		t.SpikeRateKbps = 18
	}
	return t
}
