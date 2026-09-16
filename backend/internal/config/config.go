// Package config carga la configuracion de la app desde variables de entorno.
// Regla del proyecto: ningun valor sensible vive en el codigo ni en la imagen Docker.
package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/joho/godotenv"
)

type Config struct {
	Env         string
	Port        string
	DatabaseURL string
	JWTSecret   []byte
	JWTExpiry   time.Duration
	CORSOrigins []string
	UploadsDir  string
}

// Load lee el .env (si existe) y luego las variables de entorno reales.
//
// Nota importante: godotenv NO sobreescribe variables que ya existen en el
// entorno. Eso es justo lo que queremos: en local manda el archivo .env, y
// dentro de Docker mandan las variables que inyecta docker-compose.
func Load() (*Config, error) {
	// Si el archivo no existe simplemente seguimos: en produccion (la Raspberry)
	// las variables llegan desde compose, no desde un archivo.
	_ = godotenv.Load()

	cfg := &Config{
		Env:        getEnv("APP_ENV", "development"),
		Port:       getEnv("PORT", "8080"),
		UploadsDir: getEnv("UPLOADS_DIR", "./uploads"),
	}

	var err error

	if cfg.DatabaseURL, err = requireEnv("DATABASE_URL"); err != nil {
		return nil, err
	}

	secret, err := requireEnv("JWT_SECRET")
	if err != nil {
		return nil, err
	}
	// Un secreto corto hace el JWT crackeable por fuerza bruta. Fallamos al
	// arrancar en vez de descubrirlo cuando ya este desplegado.
	if len(secret) < 32 {
		return nil, fmt.Errorf("JWT_SECRET debe tener al menos 32 caracteres (tiene %d)", len(secret))
	}
	cfg.JWTSecret = []byte(secret)

	hours, err := strconv.Atoi(getEnv("JWT_EXPIRY_HOURS", "24"))
	if err != nil || hours <= 0 {
		return nil, fmt.Errorf("JWT_EXPIRY_HOURS debe ser un entero positivo")
	}
	cfg.JWTExpiry = time.Duration(hours) * time.Hour

	origins := getEnv("CORS_ORIGINS", "http://localhost:5173")
	for _, o := range strings.Split(origins, ",") {
		if o = strings.TrimSpace(o); o != "" {
			cfg.CORSOrigins = append(cfg.CORSOrigins, o)
		}
	}
	if len(cfg.CORSOrigins) == 0 {
		return nil, fmt.Errorf("CORS_ORIGINS no puede quedar vacio")
	}

	return cfg, nil
}

func (c *Config) IsProduction() bool { return c.Env == "production" }

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func requireEnv(key string) (string, error) {
	v := os.Getenv(key)
	if v == "" {
		return "", fmt.Errorf("falta la variable de entorno %s", key)
	}
	return v, nil
}
