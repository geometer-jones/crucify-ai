package main

import (
	"log"
	"net/http"
	"os"
)

func main() {
	port := os.Getenv("PORT")
	if port == "" {
		port = "4173"
	}

	fs := http.FileServer(http.Dir("."))
	http.Handle("/", fs)

	log.Printf("Serving crucify-ai on :%s", port)
	log.Fatal(http.ListenAndServe(":"+port, nil))
}
