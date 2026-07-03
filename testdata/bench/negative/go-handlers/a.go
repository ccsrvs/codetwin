package fixture

import (
	"encoding/json"
	"net/http"
)

// listUsers responds with the user collection, newest first.
func listUsers(w http.ResponseWriter, r *http.Request) {
	users, err := store.AllUsers(r.Context())
	if err != nil {
		http.Error(w, "storage failure", http.StatusInternalServerError)
		return
	}
	sortUsersByCreated(users)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(users)
}
