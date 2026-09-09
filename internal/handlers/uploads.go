package handlers

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

// maxUploadBytes caps the decoded size of a single uploaded image.
const maxUploadBytes = 5 << 20

// uploadExtByType maps sniffed content types to the file extension served.
var uploadExtByType = map[string]string{
	"image/jpeg": ".jpg",
	"image/png":  ".png",
	"image/webp": ".webp",
}

// defaultUploadDir is used when API.UploadDir is empty, so tests that build
// the API with New keep working without wiring config.
const defaultUploadDir = "uploads"

// UploadFile stores a multipart image on disk and returns its local URL.
// Any authenticated user may upload; the returned path is then used as
// photo_url or proof_url.
func (a *API) UploadFile(w http.ResponseWriter, r *http.Request) {
	// Multipart overhead (boundaries, headers) sits on top of the file, so
	// allow a small margin before the connection is cut.
	r.Body = http.MaxBytesReader(w, r.Body, maxUploadBytes+512<<10)
	// #nosec G120 -- the body is already capped by MaxBytesReader above and
	// the file size is enforced again via fh.Size below; ParseMultipartForm
	// only controls the in-memory vs on-disk buffering threshold here.
	if err := r.ParseMultipartForm(maxUploadBytes); err != nil {
		var mbe *http.MaxBytesError
		if errors.As(err, &mbe) {
			writeError(w, http.StatusRequestEntityTooLarge, "request body too large")
			return
		}
		writeError(w, http.StatusBadRequest, "invalid multipart body")
		return
	}

	f, fh, err := r.FormFile("file")
	if err != nil {
		writeError(w, http.StatusBadRequest, "file is required")
		return
	}
	defer func() { _ = f.Close() }()

	// ParseMultipartForm buffers to disk, so MaxBytesReader alone does not
	// reject an oversized part: enforce the file size explicitly.
	if fh.Size > maxUploadBytes {
		writeError(w, http.StatusRequestEntityTooLarge, "request body too large")
		return
	}

	head := make([]byte, 512)
	n, err := io.ReadFull(f, head)
	if err != nil && !errors.Is(err, io.EOF) && !errors.Is(err, io.ErrUnexpectedEOF) {
		serverError(w, err)
		return
	}
	ext, ok := uploadExtByType[strings.Split(http.DetectContentType(head[:n]), ";")[0]]
	if !ok {
		writeError(w, http.StatusUnsupportedMediaType, "only jpg, png or webp images are allowed")
		return
	}

	var raw [16]byte
	if _, err := rand.Read(raw[:]); err != nil {
		serverError(w, err)
		return
	}
	fname := hex.EncodeToString(raw[:]) + ext

	dir := a.UploadDir
	if dir == "" {
		dir = defaultUploadDir
	}
	// #nosec G301 -- the upload directory must be readable by the static file
	// server and any reverse proxy user; uploaded files stay 0600.
	if err := os.MkdirAll(dir, 0o755); err != nil {
		serverError(w, err)
		return
	}
	// #nosec G302 G304 -- 0600 would break serving the file back over GET and
	// via a reverse proxy user. fname is a random hex name with a fixed
	// extension (never client input), so the join stays inside dir.
	dst, err := os.OpenFile(filepath.Join(dir, fname), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		serverError(w, err)
		return
	}
	if _, err := dst.Write(head[:n]); err != nil {
		_ = dst.Close()
		_ = os.Remove(dst.Name())
		serverError(w, err)
		return
	}
	if _, err := io.Copy(dst, f); err != nil {
		_ = dst.Close()
		_ = os.Remove(dst.Name())
		var mbe *http.MaxBytesError
		if errors.As(err, &mbe) {
			writeError(w, http.StatusRequestEntityTooLarge, "request body too large")
			return
		}
		serverError(w, err)
		return
	}
	if err := dst.Close(); err != nil {
		_ = os.Remove(dst.Name())
		serverError(w, err)
		return
	}

	writeJSON(w, http.StatusCreated, map[string]string{"url": "/uploads/" + fname})
}

// serveUpload serves stored images by basename. Directory listings and
// anything that is not a bare filename are 404.
func (a *API) serveUpload(w http.ResponseWriter, r *http.Request) {
	name := strings.TrimPrefix(r.URL.Path, "/uploads/")
	if name == "" || name != filepath.Base(name) || strings.Contains(name, `\`) {
		writeError(w, http.StatusNotFound, "upload not found")
		return
	}
	if !uploadExtensions[strings.ToLower(filepath.Ext(name))] {
		writeError(w, http.StatusNotFound, "upload not found")
		return
	}

	dir := a.UploadDir
	if dir == "" {
		dir = defaultUploadDir
	}
	w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	w.Header().Set("Content-Security-Policy", "default-src 'none'; img-src 'self'; style-src 'none'; frame-ancestors 'none'")
	// #nosec G703 -- name is validated above to be a bare basename with an
	// image extension (no separators, no ".."), so the join stays in dir.
	http.ServeFile(w, r, filepath.Join(dir, name))
}
