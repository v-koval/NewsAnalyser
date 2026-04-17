package config

import (
	"bufio"
	"os"
	"strconv"
	"strings"
)

type Config struct {
	HTTPAddr          string
	DatabaseURL       string
	JWTSecret         string
	AccessTTLMin      int
	RefreshTTLHours   int
	InitAdminEmail    string
	InitAdminPassword string
	StorageDir        string
	PublicBaseURL     string
}

func Load() Config {
	loadDotenv(".env")
	c := Config{
		HTTPAddr:          getenv("HTTP_ADDR", ":8080"),
		DatabaseURL:       getenv("DATABASE_URL", "postgres://postgres:postgres@localhost:5432/newsanalyzer?sslmode=disable"),
		JWTSecret:         getenv("JWT_SECRET", "dev-secret-change-me-please-32bytes"),
		AccessTTLMin:      getenvInt("ACCESS_TTL_MIN", 15),
		RefreshTTLHours:   getenvInt("REFRESH_TTL_HOURS", 720),
		InitAdminEmail:    getenv("INIT_ADMIN_EMAIL", "admin@example.com"),
		InitAdminPassword: getenv("INIT_ADMIN_PASSWORD", "admin"),
		StorageDir:        getenv("STORAGE_DIR", "./storage"),
		PublicBaseURL:     getenv("PUBLIC_BASE_URL", "http://localhost:8080"),
	}
	return c
}

func getenv(k, d string) string {
	if v, ok := os.LookupEnv(k); ok && v != "" {
		return v
	}
	return d
}

func getenvInt(k string, d int) int {
	if v, ok := os.LookupEnv(k); ok && v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return d
}

func loadDotenv(path string) {
	f, err := os.Open(path)
	if err != nil {
		return
	}
	defer f.Close()
	s := bufio.NewScanner(f)
	for s.Scan() {
		line := strings.TrimSpace(s.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		i := strings.Index(line, "=")
		if i < 0 {
			continue
		}
		k := strings.TrimSpace(line[:i])
		v := strings.TrimSpace(line[i+1:])
		v = strings.Trim(v, `"'`)
		if _, ok := os.LookupEnv(k); !ok {
			os.Setenv(k, v)
		}
	}
}
