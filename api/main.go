package main

import (
	"log"
	"net/http"
)

func main() {
	server := &Server{
		fs: &AtomixFlowStore{},
		bs: &AtomixBidStore{},
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/flows", server.getFlows)
	mux.HandleFunc("/bids", server.postBid)
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("ok"))
	})

	log.Println("HTTP telemetry API listening on :8080")
	log.Fatal(http.ListenAndServe(":8080", mux))
}
