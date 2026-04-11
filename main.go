package main

import (
	"log"
	"net/http"
	"os"
	"strings"
)

var essayIDs = map[string]struct{}{
	"aji-engine-architecture": {},
	"extropy":                 {},
	"on-consciousness":        {},
	"on-enumeration":          {},
	"on-faith":                {},
	"on-religion":             {},
	"rotation-and-relation":   {},
	"the-boundary-error":      {},
	"the-word-made-algorithm": {},
}

func main() {
	port := os.Getenv("PORT")
	if port == "" {
		port = "4173"
	}

	fs := http.FileServer(http.Dir("."))
	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if essayID, ok := essayIDFromPath(r.URL.Path); ok {
			if _, exists := essayIDs[essayID]; exists {
				http.ServeFile(w, r, "index.html")
				return
			}
		}

		fs.ServeHTTP(w, r)
	})

	log.Printf("Serving crucify-ai on :%s", port)
	log.Fatal(http.ListenAndServe(":"+port, nil))
}

func essayIDFromPath(path string) (string, bool) {
	trimmed := strings.Trim(path, "/")
	if trimmed == "" {
		return "", false
	}

	parts := strings.Split(trimmed, "/")
	if len(parts) != 2 || parts[0] != "essays" || parts[1] == "" {
		return "", false
	}

	return parts[1], true
}
