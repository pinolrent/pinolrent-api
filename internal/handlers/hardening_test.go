package handlers

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"

	"github.com/pinolrent/pinolrent-api/internal/auth"
	"github.com/pinolrent/pinolrent-api/internal/db"
)

func TestCreateReservationConcurrent(t *testing.T) {
	d, err := db.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { _ = d.Close() })

	a := auth.New("test-secret-32-bytes-minimum-okay", d)
	api := New(d, a)
	car := createCar(t, api, newSeller(t, api), map[string]any{"name": "Toyota Yaris", "price_per_day": 100})
	token := registerBuyer(t, api, "user@example.com", "secret123")

	const workers = 20
	var created, conflicts, failures atomic.Int64
	var wg sync.WaitGroup
	wg.Add(workers)

	done := make(chan struct{})
	for i := 0; i < workers; i++ {
		go func() {
			defer wg.Done()
			rec := doJSON(t, api, "POST", "/reservations", token, map[string]any{
				"car_id": car.ID, "start_date": "2026-12-01", "end_date": "2026-12-03",
			})
			switch rec.Code {
			case http.StatusCreated:
				created.Add(1)
			case http.StatusConflict:
				conflicts.Add(1)
			default:
				failures.Add(1)
			}
		}()
	}
	go func() { wg.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(20 * time.Second):
		t.Fatal("concurrent reservations timed out")
	}

	if created.Load() != 1 {
		t.Fatalf("created = %d, want 1", created.Load())
	}
	if conflicts.Load() != workers-1 {
		t.Fatalf("conflicts = %d, want %d", conflicts.Load(), workers-1)
	}
	if failures.Load() != 0 {
		t.Fatalf("unexpected failures = %d", failures.Load())
	}
}

func TestRequireAuthExpired(t *testing.T) {
	a := newTestAPI(t)
	_ = registerBuyer(t, a, "exp@example.com", "secret123")

	claims := auth.Claims{
		UserID: 1,
		Role:   "client",
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(-time.Hour)),
		},
	}
	token, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString([]byte("test-secret-32-bytes-minimum-okay"))
	if err != nil {
		t.Fatalf("sign token: %v", err)
	}

	mux := http.NewServeMux()
	mux.Handle("GET /auth/me", a.Auth.RequireAuth(func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"ok": "true"})
	}))

	req := httptest.NewRequestWithContext(context.Background(), "GET", "/auth/me", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expired token: status = %d, want 401", rec.Code)
	}
}

func TestCreateReservationPastStart(t *testing.T) {
	a := newTestAPI(t)
	car := seedCar(t, a)
	token := registerBuyer(t, a, "user@example.com", "secret123")

	yesterday := time.Now().UTC().AddDate(0, 0, -1).Format(dateLayout)
	rec := doJSON(t, a, "POST", "/reservations", token, map[string]any{
		"car_id": car.ID, "start_date": yesterday, "end_date": "2099-01-01",
	})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("past start: status = %d, want 400 (body %s)", rec.Code, rec.Body.String())
	}

	today := time.Now().UTC().Format(dateLayout)
	later := time.Now().UTC().AddDate(0, 0, 5).Format(dateLayout)
	rec = doJSON(t, a, "POST", "/reservations", token, map[string]any{
		"car_id": car.ID, "start_date": today, "end_date": later,
	})
	if rec.Code != http.StatusCreated {
		t.Fatalf("today start: status = %d, want 201 (body %s)", rec.Code, rec.Body.String())
	}
}

func TestBodyTooLarge(t *testing.T) {
	a := newTestAPI(t)

	var buf bytes.Buffer
	buf.WriteString(`{"email":"`)
	buf.WriteString(strings.Repeat("x", maxBodyBytes))
	buf.WriteString(`@example.com","password":"secret123"}`)

	req := httptest.NewRequestWithContext(context.Background(), "POST", "/auth/register", &buf)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	Routes(a).ServeHTTP(rec, req)
	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status = %d, want 413 (body %s)", rec.Code, rec.Body.String())
	}
}

func TestCarURLStrict(t *testing.T) {
	a := newTestAPI(t)
	token := newSeller(t, a)

	for _, u := range []string{"mailto:a@b.com", "/relative", "ftp://x/y.jpg", "::bad::"} {
		rec := doJSON(t, a, "POST", "/seller/cars", token, map[string]any{
			"name": "X", "price_per_day": 1, "photo_url": u,
		})
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("photo_url %q: status = %d, want 400", u, rec.Code)
		}
	}

	rec := doJSON(t, a, "POST", "/seller/cars", token, map[string]any{
		"name": "X", "price_per_day": 1, "photo_url": "https://example.com/x.jpg",
	})
	if rec.Code != http.StatusCreated {
		t.Fatalf("valid https: status = %d, want 201", rec.Code)
	}
}

func TestProofURLStrict(t *testing.T) {
	a := newTestAPI(t)
	token, _, v := seedReservation(t, a)

	rec := doJSON(t, a, "POST", "/reservations/"+itoa(v.ID)+"/payment", token, map[string]any{
		"method": "cash", "proof_url": "ftp://x/y.jpg",
	})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("proof_url ftp: status = %d, want 400", rec.Code)
	}
}

func TestCarPriceCap(t *testing.T) {
	a := newTestAPI(t)
	token := newSeller(t, a)
	rec := doJSON(t, a, "POST", "/seller/cars", token, map[string]any{
		"name": "X", "price_per_day": maxPricePerDay + 1,
	})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("over cap: status = %d, want 400", rec.Code)
	}
}

// TestRequireAuthWrongAlg rejects tokens signed with a non-HS256 algorithm,
// including the classic alg=none confusion attack.
func TestRequireAuthWrongAlg(t *testing.T) {
	a := newTestAPI(t)
	_ = registerBuyer(t, a, "alg@example.com", "secret123")

	claims := auth.Claims{
		UserID: 1,
		Role:   "client",
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    "pinolrent-api",
			Audience:  jwt.ClaimStrings{"pinolrent-api"},
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour)),
		},
	}
	token, err := jwt.NewWithClaims(jwt.SigningMethodHS384, claims).SignedString([]byte("test-secret-32-bytes-minimum-okay"))
	if err != nil {
		t.Fatalf("sign token: %v", err)
	}

	mux := http.NewServeMux()
	mux.Handle("GET /auth/me", a.Auth.RequireAuth(func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"ok": "true"})
	}))

	req := httptest.NewRequestWithContext(context.Background(), "GET", "/auth/me", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("HS384 token: status = %d, want 401", rec.Code)
	}
}

// TestRequireAuthMissingIssuer rejects tokens that lack the iss/aud claims,
// so a token signed with the same secret by another service is not accepted.
func TestRequireAuthMissingIssuer(t *testing.T) {
	a := newTestAPI(t)
	_ = registerBuyer(t, a, "iss@example.com", "secret123")

	claims := auth.Claims{
		UserID: 1,
		Role:   "client",
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour)),
		},
	}
	token, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString([]byte("test-secret-32-bytes-minimum-okay"))
	if err != nil {
		t.Fatalf("sign token: %v", err)
	}

	mux := http.NewServeMux()
	mux.Handle("GET /auth/me", a.Auth.RequireAuth(func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"ok": "true"})
	}))

	req := httptest.NewRequestWithContext(context.Background(), "GET", "/auth/me", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("no iss token: status = %d, want 401", rec.Code)
	}
}
