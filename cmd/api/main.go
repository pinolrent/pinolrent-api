package main

import (
	"crypto/rand"
	"encoding/base64"
	"log"
	"net/http"

	"github.com/pinolrent/pinolrent-api/internal/auth"
	"github.com/pinolrent/pinolrent-api/internal/config"
	"github.com/pinolrent/pinolrent-api/internal/db"
	"github.com/pinolrent/pinolrent-api/internal/handlers"
)

func main() {
	cfg := config.Load()

	if cfg.JWTSecret == "" {
		buf := make([]byte, 32)
		if _, err := rand.Read(buf); err != nil {
			log.Fatal("generate jwt secret: ", err)
		}
		cfg.JWTSecret = base64.RawURLEncoding.EncodeToString(buf)
		log.Println("JWT_SECRET not set; generated a random ephemeral secret")
	}

	d, err := db.Open(cfg.DatabaseURL)
	if err != nil {
		log.Fatal("db: ", err)
	}
	defer d.Close()

	if err := db.SeedAdmin(d, cfg.AdminEmail, cfg.AdminPassword); err != nil {
		log.Fatal("seed admin: ", err)
	}

	a := auth.New(cfg.JWTSecret, d)
	h := handlers.New(d, a)
	mux := handlers.Routes(h)

	addr := ":" + cfg.Port
	log.Printf("listening on %s", addr)
	if err := http.ListenAndServe(addr, mux); err != nil {
		log.Fatal(err)
	}
}