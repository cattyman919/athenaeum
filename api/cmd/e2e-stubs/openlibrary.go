package main

import (
	"encoding/json"
	"net/http"
)

// StubISBN is the one ISBN the Open Library stub knows (Foundation, not in the
// seeded catalog — docs/13). Any other ISBN answers `{}` (Open Library's "no
// record"), which fetch-isbn turns into a partial record.
const StubISBN = "9780553293357"

// openLibraryBooks mirrors Open Library's Books API
// GET /api/books?bibkeys=ISBN:{isbn}&jscmd=data&format=json (docs/08 §6) for
// POST /api/books/fetch-isbn (docs/03 §5.3); point OPENLIBRARY_BASE_URL at it.
func openLibraryBooks(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	key := r.URL.Query().Get("bibkeys")
	if key != "ISBN:"+StubISBN || r.URL.Query().Get("jscmd") != "data" {
		_, _ = w.Write([]byte(`{}`))
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]any{key: map[string]any{
		"title":        "Foundation",
		"key":          "/books/OL7353617M",
		"authors":      []map[string]string{{"name": "Isaac Asimov"}},
		"publishers":   []map[string]string{{"name": "Bantam Spectra"}},
		"publish_date": "1991",
		"notes":        "The first novel of Isaac Asimov's Foundation trilogy.",
		"classifications": map[string]any{
			"dewey_decimal_class": []string{"813.54"},
			"lc_classifications":  []string{"PS3551.S5 F6 1991"},
		},
	}})
}
