package handlers

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"hash/crc32"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// jpegHead sniffs as image/jpeg but carries no picture: the decoder rejects
// it, which the invalid-image test relies on.
var jpegHead = []byte{0xFF, 0xD8, 0xFF, 0xE0, 0x00, 0x10, 0x4A, 0x46, 0x49, 0x46, 0x00}

// webpHead sniffs as image/webp and is stored verbatim: the stdlib has no
// webp encoder to re-encode it with.
var webpHead = []byte{'R', 'I', 'F', 'F', 0x00, 0x00, 0x00, 0x00, 'W', 'E', 'B', 'P', 'V', 'P', '8'}

// testImage returns a picture with a per-pixel gradient, so a re-encode
// cannot be mistaken for a passthrough of the same bytes.
func testImage(w, h int) *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.Set(x, y, color.RGBA{R: uint8(x * 7), G: uint8(y * 13), B: 200, A: 255})
		}
	}
	return img
}

func encodeJPEG(t *testing.T, img image.Image) []byte {
	t.Helper()
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, nil); err != nil {
		t.Fatalf("encode jpeg: %v", err)
	}
	return buf.Bytes()
}

func encodePNG(t *testing.T, img image.Image) []byte {
	t.Helper()
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatalf("encode png: %v", err)
	}
	return buf.Bytes()
}

// jpegWithExif inserts an APP1 EXIF segment right after the SOI marker, the
// way a camera records the place a photo was taken.
func jpegWithExif(t *testing.T, base []byte) []byte {
	t.Helper()
	payload := append([]byte("Exif\x00\x00MM\x00*\x00\x00\x00\x08"), "GPS"...)
	// #nosec G115 -- the fixture payload is 17 bytes, far below the 255 a
	// one-byte length field can hold.
	seg := []byte{0xFF, 0xE1, 0x00, byte(len(payload) + 2)}
	seg = append(seg, payload...)

	out := make([]byte, 0, len(base)+len(seg))
	out = append(out, base[:2]...) // SOI
	out = append(out, seg...)
	return append(out, base[2:]...)
}

// pngWithTextChunk inserts a tEXt chunk before IDAT, standing in for the
// ancillary metadata a PNG can carry (author, software, comments).
func pngWithTextChunk(t *testing.T, base []byte) []byte {
	t.Helper()
	off := 8 // signature
	for off+8 <= len(base) {
		length := int(binary.BigEndian.Uint32(base[off : off+4]))
		if string(base[off+4:off+8]) == "IDAT" {
			break
		}
		off += 12 + length
	}

	data := append([]byte("Comment\x00"), "pinolrent"...)
	// #nosec G115 -- the fixture chunk is 17 bytes, so the cast cannot wrap.
	chunk := binary.BigEndian.AppendUint32(nil, uint32(len(data)))
	chunk = append(chunk, "tEXt"...)
	chunk = append(chunk, data...)
	chunk = binary.BigEndian.AppendUint32(chunk, crc32.ChecksumIEEE(chunk[4:]))

	out := make([]byte, 0, len(base)+len(chunk))
	out = append(out, base[:off]...)
	out = append(out, chunk...)
	return append(out, base[off:]...)
}

// pngBomb is a tiny PNG whose IHDR declares an enormous canvas. Only the
// header is real: the pixel guard has to reject it before decoding anything.
func pngBomb(w, h uint32) []byte {
	var ihdr []byte
	ihdr = binary.BigEndian.AppendUint32(ihdr, w)
	ihdr = binary.BigEndian.AppendUint32(ihdr, h)
	ihdr = append(ihdr, 8, 2, 0, 0, 0) // depth, color type, compression, filter, interlace

	out := []byte{0x89, 'P', 'N', 'G', 0x0D, 0x0A, 0x1A, 0x0A}
	// #nosec G115 -- an IHDR payload is always 13 bytes.
	out = binary.BigEndian.AppendUint32(out, uint32(len(ihdr)))
	out = append(out, "IHDR"...)
	out = append(out, ihdr...)
	return binary.BigEndian.AppendUint32(out, crc32.ChecksumIEEE(out[len(out)-17:]))
}

// storedBytes reads back a file written by an upload, by its response URL.
func storedBytes(t *testing.T, a *API, url string) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(a.UploadDir, filepath.Base(url)))
	if err != nil {
		t.Fatalf("read stored upload %s: %v", url, err)
	}
	return data
}

// uploadImage uploads content and returns the URL the API answered with.
func uploadImage(t *testing.T, a *API, token string, content []byte) string {
	t.Helper()
	rec := doUpload(t, a, token, "file", "photo", content)
	if rec.Code != http.StatusCreated {
		t.Fatalf("upload: status = %d, want 201 (body %s)", rec.Code, rec.Body.String())
	}
	var out struct {
		URL string `json:"url"`
	}
	decodeJSON(t, rec, &out)
	return out.URL
}

func assertNoUploads(t *testing.T, a *API) {
	t.Helper()
	entries, err := os.ReadDir(a.UploadDir)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("read upload dir: %v", err)
	}
	if len(entries) != 0 {
		t.Fatalf("upload dir holds %d files, want none", len(entries))
	}
}

func newUploadAPI(t *testing.T) *API {
	t.Helper()
	a := newTestAPI(t)
	a.UploadDir = t.TempDir()
	return a
}

func doUpload(t *testing.T, a *API, token, fieldName, fileName string, content []byte) *httptest.ResponseRecorder {
	t.Helper()
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	if fieldName != "" {
		fw, err := w.CreateFormFile(fieldName, fileName)
		if err != nil {
			t.Fatalf("create form file: %v", err)
		}
		if _, err := fw.Write(content); err != nil {
			t.Fatalf("write part: %v", err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatalf("close writer: %v", err)
	}

	req := httptest.NewRequestWithContext(context.Background(), "POST", "/uploads", &buf)
	req.Header.Set("Content-Type", w.FormDataContentType())
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	rec := httptest.NewRecorder()
	Routes(a).ServeHTTP(rec, req)
	return rec
}

func TestUploadImageTypes(t *testing.T) {
	img := testImage(24, 16)
	webp := append(append([]byte{}, webpHead...), bytes.Repeat([]byte{0x41}, 1024)...)

	for _, tc := range []struct {
		name     string
		content  []byte
		ext      string
		passthru bool
	}{
		{"jpeg", encodeJPEG(t, img), ".jpg", false},
		{"png", encodePNG(t, img), ".png", false},
		{"webp", webp, ".webp", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a := newUploadAPI(t)
			token := registerBuyer(t, a, "buyer@example.com", "secret123")

			rec := doUpload(t, a, token, "file", "photo.jpg", tc.content)
			if rec.Code != http.StatusCreated {
				t.Fatalf("status = %d, want 201 (body %s)", rec.Code, rec.Body.String())
			}
			var out struct {
				URL string `json:"url"`
			}
			decodeJSON(t, rec, &out)
			if !strings.HasPrefix(out.URL, "/uploads/") || !strings.HasSuffix(out.URL, tc.ext) {
				t.Fatalf("unexpected url %q", out.URL)
			}

			get := httptest.NewRequestWithContext(context.Background(), "GET", out.URL, nil)
			getRec := httptest.NewRecorder()
			Routes(a).ServeHTTP(getRec, get)
			if getRec.Code != http.StatusOK {
				t.Fatalf("GET %s: status = %d, want 200", out.URL, getRec.Code)
			}
			if ct := getRec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "image/") {
				t.Fatalf("Content-Type = %q, want image/*", ct)
			}
			if cc := getRec.Header().Get("Cache-Control"); cc == "" {
				t.Fatal("missing Cache-Control on served upload")
			}

			body := getRec.Body.Bytes()
			if tc.passthru {
				if !bytes.Equal(body, tc.content) {
					t.Fatal("passthrough format changed the bytes")
				}
				return
			}
			cfg, _, err := image.DecodeConfig(bytes.NewReader(body))
			if err != nil {
				t.Fatalf("stored image does not decode: %v", err)
			}
			if cfg.Width != 24 || cfg.Height != 16 {
				t.Fatalf("stored dimensions = %dx%d, want 24x16", cfg.Width, cfg.Height)
			}
		})
	}
}

func TestUploadRequiresAuth(t *testing.T) {
	a := newUploadAPI(t)
	rec := doUpload(t, a, "", "file", "photo.jpg", jpegHead)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
}

func TestUploadMissingFile(t *testing.T) {
	a := newUploadAPI(t)
	token := registerBuyer(t, a, "buyer@example.com", "secret123")
	rec := doUpload(t, a, token, "", "", nil)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 (body %s)", rec.Code, rec.Body.String())
	}
}

func TestUploadRejectsNonImage(t *testing.T) {
	a := newUploadAPI(t)
	token := registerBuyer(t, a, "buyer@example.com", "secret123")
	rec := doUpload(t, a, token, "file", "photo.jpg", []byte("hello, not an image at all"))
	if rec.Code != http.StatusUnsupportedMediaType {
		t.Fatalf("text as jpg: status = %d, want 415 (body %s)", rec.Code, rec.Body.String())
	}
}

func TestUploadRejectsUndecodableImage(t *testing.T) {
	a := newUploadAPI(t)
	token := registerBuyer(t, a, "buyer@example.com", "secret123")

	for _, tc := range []struct {
		name    string
		content []byte
	}{
		// sniffs as jpeg, but the APP0 segment is followed by garbage
		{"truncated jpeg", append(append([]byte{}, jpegHead...), bytes.Repeat([]byte{0x41}, 2048)...)},
		// a complete PNG header with no IDAT: nothing to decode
		{"header only png", pngBomb(1, 1)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rec := doUpload(t, a, token, "file", "photo.jpg", tc.content)
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want 400 (body %s)", rec.Code, rec.Body.String())
			}
			assertNoUploads(t, a)
		})
	}
}

func TestUploadRejectsDecompressionBomb(t *testing.T) {
	a := newUploadAPI(t)
	token := registerBuyer(t, a, "buyer@example.com", "secret123")

	// 30000x30000 = 900 MP declared in a 45-byte file
	rec := doUpload(t, a, token, "file", "bomb.png", pngBomb(30000, 30000))
	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status = %d, want 413 (body %s)", rec.Code, rec.Body.String())
	}
	assertNoUploads(t, a)
}

func TestUploadStripsEXIF(t *testing.T) {
	a := newUploadAPI(t)
	token := registerBuyer(t, a, "buyer@example.com", "secret123")

	content := jpegWithExif(t, encodeJPEG(t, testImage(24, 16)))
	if !bytes.Contains(content, []byte("Exif")) {
		t.Fatal("fixture has no EXIF segment to strip")
	}

	body := storedBytes(t, a, uploadImage(t, a, token, content))
	if bytes.Contains(body, []byte("Exif")) {
		t.Fatal("EXIF segment survived the upload")
	}
	cfg, _, err := image.DecodeConfig(bytes.NewReader(body))
	if err != nil {
		t.Fatalf("stored image does not decode: %v", err)
	}
	if cfg.Width != 24 || cfg.Height != 16 {
		t.Fatalf("stored dimensions = %dx%d, want 24x16", cfg.Width, cfg.Height)
	}
}

func TestUploadStripsPNGMetadata(t *testing.T) {
	a := newUploadAPI(t)
	token := registerBuyer(t, a, "buyer@example.com", "secret123")

	content := pngWithTextChunk(t, encodePNG(t, testImage(24, 16)))
	if !bytes.Contains(content, []byte("tEXt")) {
		t.Fatal("fixture has no tEXt chunk to strip")
	}

	body := storedBytes(t, a, uploadImage(t, a, token, content))
	if bytes.Contains(body, []byte("tEXt")) {
		t.Fatal("tEXt chunk survived the upload")
	}
	if bytes.Contains(body, []byte("pinolrent")) {
		t.Fatal("chunk payload survived the upload")
	}
	if _, _, err := image.Decode(bytes.NewReader(body)); err != nil {
		t.Fatalf("stored image does not decode: %v", err)
	}
}

func TestUploadTooLarge(t *testing.T) {
	a := newUploadAPI(t)
	token := registerBuyer(t, a, "buyer@example.com", "secret123")
	big := append(append([]byte{}, jpegHead...), bytes.Repeat([]byte{0x41}, maxUploadBytes)...)
	rec := doUpload(t, a, token, "file", "big.jpg", big)
	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status = %d, want 413 (body %s)", rec.Code, rec.Body.String())
	}
}

func TestServeUploadNotFound(t *testing.T) {
	a := newUploadAPI(t)
	for _, path := range []string{"/uploads/", "/uploads/sub/x.jpg", "/uploads/x.txt"} {
		req := httptest.NewRequestWithContext(context.Background(), "GET", path, nil)
		rec := httptest.NewRecorder()
		Routes(a).ServeHTTP(rec, req)
		if rec.Code != http.StatusNotFound {
			t.Fatalf("GET %s: status = %d, want 404", path, rec.Code)
		}
	}
}

func TestUploadQuota(t *testing.T) {
	a := newUploadAPI(t)
	token := registerBuyer(t, a, "buyer@example.com", "secret123")
	content := encodeJPEG(t, testImage(24, 16))

	// default (0) means unlimited: the file lands
	if rec := doUpload(t, a, token, "file", "photo.jpg", content); rec.Code != http.StatusCreated {
		t.Fatalf("unlimited: status = %d, want 201 (body %s)", rec.Code, rec.Body.String())
	}
	used, err := dirUsageBytes(a.UploadDir)
	if err != nil {
		t.Fatalf("usage: %v", err)
	}

	// room for exactly one more upload of this size: fits
	a.UploadMaxTotal = used + int64(len(content))
	if rec := doUpload(t, a, token, "file", "photo.jpg", content); rec.Code != http.StatusCreated {
		t.Fatalf("exact fit: status = %d, want 201 (body %s)", rec.Code, rec.Body.String())
	}

	// the file just stored takes space, so the next one no longer fits
	if rec := doUpload(t, a, token, "file", "photo.jpg", content); rec.Code != http.StatusInsufficientStorage {
		t.Fatalf("over quota: status = %d, want 507 (body %s)", rec.Code, rec.Body.String())
	}
}

func TestCleanupOrphanUploads(t *testing.T) {
	a := newUploadAPI(t)
	seller := newSeller(t, a)
	ctx := context.Background()

	put := func(name string, age time.Duration) string {
		t.Helper()
		path := filepath.Join(a.UploadDir, name)
		if err := os.WriteFile(path, []byte("x"), 0o600); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
		if age > 0 {
			when := time.Now().Add(-age)
			if err := os.Chtimes(path, when, when); err != nil {
				t.Fatalf("chtimes %s: %v", name, err)
			}
		}
		return path
	}
	past := orphanGrace + time.Hour

	carRef := put("car-ref.jpg", past)
	payRef := put("pay-ref.jpg", past)
	orphan := put("orphan.jpg", past)
	young := put("young.jpg", 0)
	other := put("notes.txt", past)

	// rows that reference two of the files
	createCar(t, a, seller, map[string]any{
		"name": "Con foto", "price_per_day": 100, "photo_url": "/uploads/car-ref.jpg",
	})
	buyer, _, v := seedReservation(t, a)
	doJSON(t, a, "POST", "/reservations/"+itoa(v.ID)+"/payment", buyer, map[string]any{
		"method": "cash", "proof_url": "/uploads/pay-ref.jpg",
	})

	if err := a.CleanupOrphanUploads(ctx); err != nil {
		t.Fatalf("cleanup: %v", err)
	}

	exists := func(path string) bool {
		_, err := os.Stat(path)
		return err == nil
	}
	if !exists(carRef) {
		t.Fatal("file referenced by a car photo was removed")
	}
	if !exists(payRef) {
		t.Fatal("file referenced by a payment proof was removed")
	}
	if exists(orphan) {
		t.Fatal("orphan past the grace period was not removed")
	}
	if !exists(young) {
		t.Fatal("orphan inside the grace period was removed")
	}
	if !exists(other) {
		t.Fatal("non-image file was removed")
	}
}

func TestLocalUploadURLAccepted(t *testing.T) {
	a := newUploadAPI(t)
	seller := newSeller(t, a)
	url := "/uploads/" + strings.Repeat("a", 16) + ".jpg"
	rec := doJSON(t, a, "POST", "/seller/cars", seller, map[string]any{
		"name": "X", "price_per_day": 1, "photo_url": url,
	})
	if rec.Code != http.StatusCreated {
		t.Fatalf("photo_url %q: status = %d, want 201 (body %s)", url, rec.Code, rec.Body.String())
	}

	token, _, v := seedReservation(t, a)
	rec = doJSON(t, a, "POST", "/reservations/"+itoa(v.ID)+"/payment", token, map[string]any{
		"method": "cash", "proof_url": url,
	})
	if rec.Code != http.StatusCreated {
		t.Fatalf("proof_url %q: status = %d, want 201 (body %s)", url, rec.Code, rec.Body.String())
	}

	for _, u := range []string{"/uploads/../x.jpg", "/uploads/x.exe", "/uploads/", "/uploads/sub/x.jpg"} {
		rec := doJSON(t, a, "POST", "/seller/cars", seller, map[string]any{
			"name": "X", "price_per_day": 1, "photo_url": u,
		})
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("photo_url %q: status = %d, want 400", u, rec.Code)
		}
	}
}
