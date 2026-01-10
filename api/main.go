package main

import (
	"log"
	"net/http"
)

func main() {
	store := &AtomixStore{}

	server := &Server{
		store: store,
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/flows", server.getFlows)
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("ok"))
	})

	log.Println("HTTP telemetry API listening on :8080")
	log.Fatal(http.ListenAndServe(":8080", mux))
}
