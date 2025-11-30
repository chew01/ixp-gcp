package main

import (
	"fmt"
	"io"
	"log"
	"net/http"
)

func main() {
	http.HandleFunc("/callback", func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		fmt.Println("Received callback:", string(body))
		w.Write([]byte("ok"))
	})

	fmt.Println("Callback server running on :8080")
	log.Fatal(http.ListenAndServe(":8080", nil))
}
