package main

import (
	"context"
	"log"

	"github.com/chew01/ixp-gcp/proto"
)

type DummySwitch struct {
	proto.UnimplementedVirtualCircuitServer
}

func (s *DummySwitch) SetUp(ctx context.Context, req *proto.SetUpRequest) (*proto.SetUpResponse, error) {
	log.Printf("SetUp called with request: %+v", req)
	return &proto.SetUpResponse{
		IsSuccess: true,
	}, nil
}

func (s *DummySwitch) TearDown(ctx context.Context, req *proto.TearDownRequest) (*proto.TearDownResponse, error) {
	log.Printf("TearDown called with request: %+v", req)
	return &proto.TearDownResponse{
		IsSuccess: true,
	}, nil
}
