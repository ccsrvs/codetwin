package fixture

import (
	"encoding/json"
	"net/http"
)

// createOrder validates the payload and persists a new order.
func createOrder(w http.ResponseWriter, r *http.Request) {
	var req orderRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "bad payload", http.StatusBadRequest)
		return
	}
	id, err := store.InsertOrder(r.Context(), req.Items, req.CustomerID)
	if err != nil {
		http.Error(w, "storage failure", http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(map[string]string{"id": id})
}
