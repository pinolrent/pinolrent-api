package config

import "os"

type Config struct {
	Port          string
	DatabaseURL   string
	JWTSecret     string
	AdminEmail    string
	AdminPassword string
}

func Load() Config {
	return Config{
		Port:          getenv("PORT", "8080"),
		DatabaseURL:   getenv("DATABASE_URL", "pinolrent.db"),
		JWTSecret:     os.Getenv("JWT_SECRET"),
		AdminEmail:    getenv("ADMIN_EMAIL", "admin@pinolrent.com"),
		AdminPassword: getenv("ADMIN_PASSWORD", "admin123"),
	}
}

func getenv(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}