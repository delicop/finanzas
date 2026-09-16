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

	// TokenMantenimiento protege las rutas para revisar la bitacora de
	// errores desde afuera. Si queda vacio, esas rutas NO se montan.
	TokenMantenimiento string

	// LLM configura el agente conversacional. Sin llave no se monta: la app
	// entera funciona igual, simplemente no tiene chat.
	LLM LLMConfig
}

// LLMConfig apunta a cualquier API compatible con la de OpenAI. Hoy DeepSeek,
// manana OpenRouter u otra: se cambian dos variables y no se toca codigo.
type LLMConfig struct {
	BaseURL string
	APIKey  string
	Modelo  string

	// Timeout de la llamada al proveedor. Tiene que quedar POR DEBAJO del
	// middleware.Timeout del router (30s): si no, el request se corta antes y
	// el usuario ve un error generico en vez del "no está disponible" nuestro.
	Timeout time.Duration

	// LimiteDiario son los mensajes que puede mandar cada usuario en 24 horas.
	LimiteDiario int
}

// Habilitado: sin llave el chat no existe. Es el mismo criterio que
// TOKEN_MANTENIMIENTO — una funcionalidad opcional se apaga sola cuando no
// esta configurada, en vez de arrancar a medias y fallar en la primera
// peticion.
func (l LLMConfig) Habilitado() bool { return l.APIKey != "" }

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

	// Opcional: sin el, las rutas de mantenimiento no existen.
	cfg.TokenMantenimiento = os.Getenv("TOKEN_MANTENIMIENTO")
	if cfg.TokenMantenimiento != "" && len(cfg.TokenMantenimiento) < 24 {
		return nil, fmt.Errorf("TOKEN_MANTENIMIENTO debe tener al menos 24 caracteres (tiene %d)", len(cfg.TokenMantenimiento))
	}

	if cfg.LLM, err = cargarLLM(); err != nil {
		return nil, err
	}

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

// cargarLLM lee la configuracion del agente. Si no hay llave devuelve la
// estructura vacia sin validar nada mas: el chat queda apagado y punto.
func cargarLLM() (LLMConfig, error) {
	llm := LLMConfig{APIKey: os.Getenv("LLM_API_KEY")}
	if !llm.Habilitado() {
		return llm, nil
	}

	llm.BaseURL = getEnv("LLM_BASE_URL", "https://api.deepseek.com/v1")
	if !strings.HasPrefix(llm.BaseURL, "http://") && !strings.HasPrefix(llm.BaseURL, "https://") {
		return llm, fmt.Errorf("LLM_BASE_URL debe empezar por http:// o https:// (es %q)", llm.BaseURL)
	}

	llm.Modelo = getEnv("LLM_MODELO", "deepseek-chat")

	segundos, err := strconv.Atoi(getEnv("LLM_TIMEOUT_SEGUNDOS", "25"))
	// El techo de 25s no es arbitrario: el router corta toda peticion a los
	// 30s. Un timeout mayor nunca llegaria a cumplirse.
	if err != nil || segundos < 5 || segundos > 25 {
		return llm, fmt.Errorf("LLM_TIMEOUT_SEGUNDOS debe ser un entero entre 5 y 25")
	}
	llm.Timeout = time.Duration(segundos) * time.Second

	llm.LimiteDiario, err = strconv.Atoi(getEnv("LLM_LIMITE_DIARIO", "50"))
	if err != nil || llm.LimiteDiario <= 0 {
		return llm, fmt.Errorf("LLM_LIMITE_DIARIO debe ser un entero positivo")
	}

	return llm, nil
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
