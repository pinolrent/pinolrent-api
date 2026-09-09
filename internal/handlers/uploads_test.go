package handlers

import (
	"bytes"
	"context"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
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
