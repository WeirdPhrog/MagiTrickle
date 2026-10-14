package utils

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"magitrickle/api/types"
)

const maxJSONBody = 32 << 20

func WriteJson(w http.ResponseWriter, httpCode int, data interface{}) {
	buf, err := json.Marshal(data)
	if err != nil {
		WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(httpCode)
	w.Write(buf)
}

func WriteError(w http.ResponseWriter, httpCode int, e string) {
	WriteJson(w, httpCode, types.ErrorRes{Error: e})
}

func ReadJson[T any](r *http.Request) (T, error) {
	var req T
	decoder := json.NewDecoder(http.MaxBytesReader(nil, r.Body, maxJSONBody))
	if err := decoder.Decode(&req); err != nil {
		return req, fmt.Errorf("failed to parse request: %w", err)
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		if err == nil {
			err = fmt.Errorf("expected a single JSON value")
		}
		return req, fmt.Errorf("failed to parse request: %w", err)
	}
	return req, nil
}
