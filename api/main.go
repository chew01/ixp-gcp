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

	appMux := http.NewServeMux()
	appMux.HandleFunc("/flows", server.getFlows)
	appMux.HandleFunc("/bids", server.postBid)
	appMux.HandleFunc("/metrics", server.getMetrics)

	metricsMux := http.NewServeMux()
	metricsMux.HandleFunc("/metrics", server.getMetrics)

	go func() {
		log.Println("Metrics server listening on :9090")
		log.Fatal(http.ListenAndServe(":9090", metricsMux))
	}()

	log.Println("API Gateway listening on :8080")
	log.Fatal(http.ListenAndServe(":8080", appMux))
}
