package auth

import (
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
)

// limitador frena los intentos de login por IP (ventana fija).
//
// Por que existe: la app va a quedar expuesta en una Raspberry con UN solo
// usuario, o sea un solo email valido que adivinar. Sin este freno, un script
// puede probar miles de contrasenas por minuto. Son ~50 lineas y elimina
// el ataque de fuerza bruta mas obvio.
//
// Es en memoria a proposito: un solo proceso, sin Redis. Si reinicias el
// contenedor se limpia, y eso aqui no es un problema real.
type limitador struct {
	mu        sync.Mutex
	intentos  map[string]*ventana
	max       int
	duracion  time.Duration
	ultimaLim time.Time
}

type ventana struct {
	conteo int
	inicio time.Time
}

func nuevoLimitador(max int, duracion time.Duration) *limitador {
	return &limitador{
		intentos: make(map[string]*ventana),
		max:      max,
		duracion: duracion,
	}
}

// permitir devuelve false cuando la IP ya gasto sus intentos en la ventana.
func (l *limitador) permitir(ip string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()

	ahora := time.Now()
	l.limpiar(ahora)

	v, existe := l.intentos[ip]
	if !existe || ahora.Sub(v.inicio) > l.duracion {
		l.intentos[ip] = &ventana{conteo: 1, inicio: ahora}
		return true
	}

	v.conteo++
	return v.conteo <= l.max
}

// exito borra el contador: un login correcto no debe dejar penalizada la IP.
func (l *limitador) exito(ip string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	delete(l.intentos, ip)
}

// limpiar descarta ventanas viejas para que el mapa no crezca indefinidamente.
// Debe llamarse con el mutex ya tomado.
func (l *limitador) limpiar(ahora time.Time) {
	if ahora.Sub(l.ultimaLim) < l.duracion {
		return
	}
	l.ultimaLim = ahora
	for ip, v := range l.intentos {
		if ahora.Sub(v.inicio) > l.duracion {
			delete(l.intentos, ip)
		}
	}
}

// ipDelRequest saca la IP del cliente.
//
// Detras de un proxy (nginx/Caddy en la Raspberry) r.RemoteAddr es la IP del
// proxy, no la del cliente. Por eso miramos X-Forwarded-For, que el proxy
// debe estar configurado para setear.
func ipDelRequest(r *http.Request) string {
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		// El primer valor de la lista es el cliente original.
		primera, _, _ := strings.Cut(xff, ",")
		if primera = strings.TrimSpace(primera); primera != "" {
			return primera
		}
	}
	ip, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return ip
}
