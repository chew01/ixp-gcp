package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"math/rand/v2"
	"net"
	"os"
	"time"

	pb "github.com/chew01/ixp-gcp/proto"
	"github.com/segmentio/kafka-go"
	"google.golang.org/grpc"
)

type DummyProducer struct {
	topic    string
	switchID string
	kafka    *kafka.Writer
}

type DummySwitch struct {
	pb.UnimplementedVirtualCircuitServer
}

type Flow struct {
	IngressPort int    `json:"ingress_port"`
	EgressPort  int    `json:"egress_port"`
	VlanID      int    `json:"vlan_id"`
	Bytes       uint64 `json:"bytes"`
}

type Record struct {
	SchemaVersion int    `json:"schema_version"`
	SwitchID      string `json:"switch_id"`
	WindowStartNS int64  `json:"window_start_ns"`
	WindowEndNS   int64  `json:"window_end_ns"`
	Flows         []Flow `json:"flows"`
}

const Topic = "switch-traffic-digests"
const SwitchId = "sw-1"
const FlowsPerWindow = 5
const WindowSec = 1

func main() {
	kafkaBootstrap := os.Getenv("KAFKA_BOOTSTRAP")
	if kafkaBootstrap == "" {
		log.Fatal("KAFKA_BOOTSTRAP env var not set")
	}

	writer := kafka.Writer{
		Addr:     kafka.TCP(kafkaBootstrap),
		Topic:    Topic,
		Balancer: &kafka.LeastBytes{},
	}
	defer writer.Close()

	producer := DummyProducer{switchID: SwitchId, kafka: &writer}
	s := grpc.NewServer()
	pb.RegisterVirtualCircuitServer(s, &DummySwitch{})

	lis, err := net.Listen("tcp", ":50051")
	if err != nil {
		log.Fatalf("failed to listen: %v", err)
	}

	ctx := context.Background()
	go producer.Run(ctx)

	if err := s.Serve(lis); err != nil {
		log.Fatalf("failed to serve: %v", err)
	}
}

func (p *DummyProducer) Run(ctx context.Context) {
	for {
		windowStartNs := time.Now().UnixNano()
		time.Sleep(WindowSec * time.Second)
		windowEndNs := time.Now().UnixNano()

		flows := make([]Flow, FlowsPerWindow)
		for i := 0; i < FlowsPerWindow; i++ {
			f := Flow{
				IngressPort: randRange(1, 4),
				EgressPort:  randRange(5, 8),
				VlanID:      randChoice([]int{100, 200, 300}),
				Bytes:       uint64(randRange(5e5, 2e6)),
			}
			flows[i] = f
		}

		r := Record{
			SchemaVersion: 1,
			SwitchID:      SwitchId,
			WindowStartNS: windowStartNs,
			WindowEndNS:   windowEndNs,
			Flows:         flows,
		}

		key := fmt.Sprintf("%s|%d", SwitchId, windowStartNs)
		value, err := json.Marshal(r)
		if err != nil {
			log.Fatal(err)
		}

		err = p.kafka.WriteMessages(ctx, kafka.Message{
			Key:   []byte(key),
			Value: value,
		})
		if err != nil {
			log.Fatal(err)
		}
		log.Printf("Produced %d flows", len(flows))
	}
}

func (s *DummySwitch) SetUp(ctx context.Context, req *pb.SetUpRequest) (*pb.SetUpResponse, error) {
	log.Printf("SetUp called with request: %+v", req)
	return &pb.SetUpResponse{}, nil
}

func (s *DummySwitch) TearDown(ctx context.Context, req *pb.TearDownRequest) (*pb.TearDownResponse, error) {
	log.Printf("TearDown called with request: %+v", req)
	return &pb.TearDownResponse{}, nil
}

// randRange returns random number in range min to max inclusive
func randRange(min int, max int) int {
	return rand.IntN(max+1-min) + min
}

func randChoice(choices []int) int {
	return choices[rand.IntN(len(choices))]
}
