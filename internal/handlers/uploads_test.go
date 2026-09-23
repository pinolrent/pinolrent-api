package handlers

import (
	"bytes"
	"context"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Minimal magic bytes accepted by http.DetectContentType for each type.
var (
	jpegHead = []byte{0xFF, 0xD8, 0xFF, 0xE0, 0x00, 0x10, 0x4A, 0x46, 0x49, 0x46, 0x00}
	pngHead  = []byte{0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A, 0x00, 0x00, 0x00, 0x0D}
	webpHead = []byte{'R', 'I', 'F', 'F', 0x00, 0x00, 0x00, 0x00, 'W', 'E', 'B', 'P', 'V', 'P', '8'}
)

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
	for _, tc := range []struct {
		name string
		head []byte
		ext  string
	}{
		{"jpeg", jpegHead, ".jpg"},
		{"png", pngHead, ".png"},
		{"webp", webpHead, ".webp"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a := newUploadAPI(t)
			token := registerBuyer(t, a, "buyer@example.com", "secret123")

			content := append(append([]byte{}, tc.head...), bytes.Repeat([]byte{0x41}, 1024)...)
			rec := doUpload(t, a, token, "file", "photo.jpg", content)
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
			if !bytes.Equal(getRec.Body.Bytes(), content) {
				t.Fatal("served bytes differ from uploaded content")
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
	content := append(append([]byte{}, jpegHead...), bytes.Repeat([]byte{0x41}, 2048)...)

	// default (0) means unlimited: the file lands
	if rec := doUpload(t, a, token, "file", "photo.jpg", content); rec.Code != http.StatusCreated {
		t.Fatalf("unlimited: status = %d, want 201 (body %s)", rec.Code, rec.Body.String())
	}
	firstSize := int64(len(content))

	// a quota exactly covering usage plus one more small file: fits
	a.UploadMaxTotal = firstSize + int64(len(jpegHead))
	if rec := doUpload(t, a, token, "file", "photo.jpg", jpegHead); rec.Code != http.StatusCreated {
		t.Fatalf("exact fit: status = %d, want 201 (body %s)", rec.Code, rec.Body.String())
	}

	// one byte past the quota: rejected before touching the disk
	if rec := doUpload(t, a, token, "file", "photo.jpg", jpegHead); rec.Code != http.StatusInsufficientStorage {
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
