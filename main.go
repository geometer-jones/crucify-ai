package main

import (
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

var essayRouteAliases = map[string]string{
	"on-consciousness": "self-architecture",
}

func main() {
	port := os.Getenv("PORT")
	if port == "" {
		port = "4173"
	}

	essayIDs := loadEssayIDs("essays")
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

func loadEssayIDs(dir string) map[string]struct{} {
	entries, err := os.ReadDir(dir)
	if err != nil {
		log.Fatalf("load essay ids: %v", err)
	}

	ids := make(map[string]struct{}, len(entries)+len(essayRouteAliases))
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".html" {
			continue
		}

		id := strings.TrimSuffix(entry.Name(), ".html")
		if id == "" {
			continue
		}

		ids[id] = struct{}{}
	}

	for alias := range essayRouteAliases {
		ids[alias] = struct{}{}
	}

	return ids
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
