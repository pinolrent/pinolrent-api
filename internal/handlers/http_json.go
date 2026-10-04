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

// statusError is a business-rule outcome, not a bug: the request is understood
// but cannot be fulfilled (unknown id, wrong state, conflict). Transaction
// callbacks return it instead of writing a response, so the transaction rolls
// back and the handler maps it to its status in one place. Anything else a
// callback returns is a server error.
type statusError struct {
	status int
	msg    string
}

func (e *statusError) Error() string { return e.msg }

// writeTxErr maps a WithImmediateTx result to its response: nil means success
// and the caller responds, a statusError becomes its status, anything else is
// a 500. It reports whether the response was written.
func writeTxErr(w http.ResponseWriter, err error) bool {
	if err == nil {
		return false
	}
	var se *statusError
	if errors.As(err, &se) {
		writeError(w, se.status, se.msg)
		return true
	}
	serverError(w, err)
	return true
}
