package optutils

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
)

func ReadJSON(r *http.Request, v any) error {
	if r.Body == nil {
		return fmt.Errorf("missing request body")
	}
	return json.NewDecoder(r.Body).Decode(v)
}

func WriteJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func PathValueString(r *http.Request, name string) (string, error) {
	pathValue := r.PathValue(name)
	if pathValue == "" {
		return "", fmt.Errorf("empty path value for name: %s", name)
	}
	return pathValue, nil
}

func PathValueI32(r *http.Request, name string) (int, error) {
	pathValue := r.PathValue(name)
	if pathValue == "" {
		return 0, fmt.Errorf("empty path value for name: %s", name)
	}
	return strconv.Atoi(pathValue)
}

func PathValueI64(r *http.Request, name string) (int64, error) {
	pathValue := r.PathValue(name)
	if pathValue == "" {
		return 0, fmt.Errorf("empty path value for name: %s", name)
	}
	return strconv.ParseInt(pathValue, 10, 64)
}
