package handlers

import (
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"

	"github.com/pinolrent/pinolrent-api/internal/httpx"
)

const maxBodyBytes = 1 << 20

func writeJSON(w http.ResponseWriter, status int, v any) {
	httpx.WriteJSON(w, status, v)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	httpx.WriteError(w, status, msg)
}

func serverError(w http.ResponseWriter, err error) {
	slog.Error("internal error", "error", err)
	httpx.WriteError(w, http.StatusInternalServerError, "server error")
}

var errBodyTooLarge = errors.New("request body too large")

func decodeBody(w http.ResponseWriter, r *http.Request, dst any) error {
	r.Body = http.MaxBytesReader(w, r.Body, maxBodyBytes)
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		var mbe *http.MaxBytesError
		if errors.As(err, &mbe) {
			return errBodyTooLarge
		}
		return errors.New("invalid JSON body")
	}
	if err := dec.Decode(new(struct{})); !errors.Is(err, io.EOF) {
		return errors.New("invalid JSON body")
	}
	return nil
}

func writeBodyErr(w http.ResponseWriter, err error) {
	if errors.Is(err, errBodyTooLarge) {
		writeError(w, http.StatusRequestEntityTooLarge, "request body too large")
		return
	}
	writeError(w, http.StatusBadRequest, "invalid JSON body")
}
