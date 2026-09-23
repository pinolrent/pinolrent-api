package handlers

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"image"
	"image/jpeg"
	"image/png"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// maxUploadBytes caps the decoded size of a single uploaded image.
const maxUploadBytes = 5 << 20

// maxDecodePixels caps the canvas an image may declare before it is decoded.
// A few hundred bytes of PNG header can claim a canvas of millions of pixels,
// and decoding it allocates width*height*4 bytes: the cap keeps the worst case
// around 200 MB. It sits far above any real camera (a 48 MP phone sensor
// outputs roughly 8000x6000, i.e. 48 million pixels).
const maxDecodePixels = 50_000_000

// jpegQuality is the quality used when re-encoding a jpeg upload. 85 is the
// usual "visually lossless" point for photos.
const jpegQuality = 85

// Errors that stripImageMetadata reports so the handler can map them to a
// status without leaking decoder internals to the client.
var (
	errImageTooLarge    = errors.New("image dimensions too large")
	errInvalidImageData = errors.New("invalid image data")
)

// orphanGrace is how long an unreferenced file survives the cleanup sweep:
// long enough for the slowest flow (upload now, attach it to a car or a
// payment days later) to still find its file on disk.
const orphanGrace = 7 * 24 * time.Hour

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
	// enforce the file size: check it explicitly.
	if fh.Size > maxUploadBytes {
		writeError(w, http.StatusRequestEntityTooLarge, "request body too large")
		return
	}

	dir := a.UploadDir
	if dir == "" {
		dir = defaultUploadDir
	}
	// Enforced before the file lands on disk. The usage scan and the write
	// are not one atomic step, so concurrent uploads can overshoot by what
	// the write limiter admits — on a single instance that burst is bounded
	// and the quota is a ceiling, not a transaction.
	if a.UploadMaxTotal > 0 {
		usage, err := dirUsageBytes(dir)
		if err != nil {
			serverError(w, err)
			return
		}
		if usage+fh.Size > a.UploadMaxTotal {
			writeError(w, http.StatusInsufficientStorage, "storage quota exceeded")
			return
		}
	}

	// The part is already fully buffered (ParseMultipartForm spools it to disk
	// past maxUploadBytes, and the size check above bounds it), so reading it
	// into memory costs at most maxUploadBytes. Decoding is the risky part,
	// not the read, which is what maxDecodePixels guards.
	data, err := io.ReadAll(f)
	if err != nil {
		serverError(w, err)
		return
	}
	if len(data) > maxUploadBytes {
		writeError(w, http.StatusRequestEntityTooLarge, "request body too large")
		return
	}

	sniff := data
	if len(sniff) > 512 {
		sniff = sniff[:512]
	}
	ext, ok := uploadExtByType[strings.Split(http.DetectContentType(sniff), ";")[0]]
	if !ok {
		writeError(w, http.StatusUnsupportedMediaType, "only jpg, png or webp images are allowed")
		return
	}

	body := data
	// jpeg and png are re-encoded from their decoded pixels, so the stored
	// file cannot carry metadata: EXIF (GPS, camera serial, timestamps) and
	// ancillary chunks are dropped by construction. webp has no encoder in
	// the stdlib, so it stays a passthrough and keeps whatever it carries.
	if ext != ".webp" {
		body, err = stripImageMetadata(data, ext)
		if err != nil {
			switch {
			case errors.Is(err, errImageTooLarge):
				writeError(w, http.StatusRequestEntityTooLarge, "image dimensions too large")
			case errors.Is(err, errInvalidImageData):
				writeError(w, http.StatusBadRequest, "invalid image data")
			default:
				serverError(w, err)
			}
			return
		}
	}

	var raw [16]byte
	if _, err := rand.Read(raw[:]); err != nil {
		serverError(w, err)
		return
	}
	fname := hex.EncodeToString(raw[:]) + ext

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
	if _, err := dst.Write(body); err != nil {
		_ = dst.Close()
		_ = os.Remove(dst.Name())
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

// stripImageMetadata re-encodes a jpeg or png from its decoded pixels. The
// stdlib encoders write pixel data only, so the result carries no EXIF (where
// a phone camera records the GPS position of, say, the seller's home) and no
// ancillary chunks such as tEXt.
func stripImageMetadata(data []byte, ext string) ([]byte, error) {
	cfg, _, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return nil, errInvalidImageData
	}
	// int64 keeps the product meaningful on 32-bit builds, where the declared
	// dimensions would overflow an int.
	if int64(cfg.Width)*int64(cfg.Height) > maxDecodePixels {
		return nil, errImageTooLarge
	}
	img, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, errInvalidImageData
	}

	var buf bytes.Buffer
	if ext == ".jpg" {
		err = jpeg.Encode(&buf, img, &jpeg.Options{Quality: jpegQuality})
	} else {
		err = png.Encode(&buf, img)
	}
	if err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// dirUsageBytes sums the size of the regular files in dir. A missing
// directory counts as empty: nothing has been uploaded yet.
func dirUsageBytes(dir string) (int64, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return 0, nil
		}
		return 0, err
	}
	var total int64
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue // removed underneath us; it does not count
		}
		total += info.Size()
	}
	return total, nil
}

// CleanupOrphanUploads deletes image files older than orphanGrace that no row
// references anymore. cars.photo_url and payments.proof_url are the only
// places an /uploads/ path can be stored, so anything else is a leftover: an
// abandoned upload or the photo of a deleted car.
func (a *API) CleanupOrphanUploads(ctx context.Context) error {
	dir := a.UploadDir
	if dir == "" {
		dir = defaultUploadDir
	}

	refs := map[string]bool{}
	rows, err := a.DB.QueryContext(ctx, `SELECT photo_url FROM cars UNION SELECT proof_url FROM payments`)
	if err != nil {
		return err
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var u string
		if err := rows.Scan(&u); err != nil {
			return err
		}
		if base, ok := cutUploadPrefix(u); ok {
			refs[base] = true
		}
	}
	if err := rows.Err(); err != nil {
		return err
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}
	deadline := time.Now().Add(-orphanGrace)
	removed := 0
	for _, e := range entries {
		if e.IsDir() || refs[e.Name()] {
			continue
		}
		if !uploadExtensions[strings.ToLower(filepath.Ext(e.Name()))] {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		if info.ModTime().After(deadline) {
			continue
		}
		if err := os.Remove(filepath.Join(dir, e.Name())); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		removed++
	}
	if removed > 0 {
		slog.Info("orphan uploads removed", "count", removed, "dir", dir)
	}
	return nil
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
