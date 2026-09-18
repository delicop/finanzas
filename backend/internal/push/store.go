package push

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
)

type Store struct {
	db *sql.DB
}

func NewStore(db *sql.DB) *Store { return &Store{db: db} }

// MaxFallos: tras tantos fallos seguidos que no son "muerta", la suscripcion
// se borra igual. Un endpoint que lleva diez avisos fallando no va a empezar
// a funcionar solo, y cada intento es una peticion HTTP que retrasa la tarea.
const MaxFallos = 10

// Guardar registra (o actualiza) la suscripcion de un navegador.
//
// ON CONFLICT sobre el endpoint, que es unico en el mundo: si el mismo
// navegador vuelve a suscribirse, es la MISMA suscripcion con llaves nuevas,
// no una segunda. Sin esto, cada vez que el navegador rota sus llaves el
// usuario recibiria el aviso dos veces, tres, cuatro...
//
// El usuario_id tambien se actualiza a proposito: un celular prestado en el
// que entra otra persona tiene que pasar a recibir SUS avisos, no los del
// dueño anterior.
func (s *Store) Guardar(ctx context.Context, usuarioID int64, sus Suscripcion) error {
	const q = `
		INSERT INTO push_suscripciones (usuario_id, endpoint, p256dh, auth)
		VALUES ($1, $2, $3, $4)
		ON CONFLICT (endpoint) DO UPDATE
		SET usuario_id = excluded.usuario_id,
		    p256dh     = excluded.p256dh,
		    auth       = excluded.auth,
		    fallos     = 0`

	if _, err := s.db.ExecContext(ctx, q, usuarioID, sus.Endpoint, sus.P256dh, sus.Auth); err != nil {
		return fmt.Errorf("guardando la suscripcion de push: %w", err)
	}
	return nil
}

// Borrar quita una suscripcion concreta (el usuario apago los avisos en este
// dispositivo). Filtra por usuario: nadie puede desuscribir el celular de otro.
func (s *Store) Borrar(ctx context.Context, usuarioID int64, endpoint string) error {
	const q = `DELETE FROM push_suscripciones WHERE usuario_id = $1 AND endpoint = $2`

	if _, err := s.db.ExecContext(ctx, q, usuarioID, endpoint); err != nil {
		return fmt.Errorf("borrando la suscripcion de push: %w", err)
	}
	return nil
}

// BorrarPorEndpoint la quita sin mirar de quien es. Solo lo llama el enviador
// cuando el servicio de push dice que ese endpoint ya no existe.
func (s *Store) BorrarPorEndpoint(ctx context.Context, endpoint string) error {
	const q = `DELETE FROM push_suscripciones WHERE endpoint = $1`

	if _, err := s.db.ExecContext(ctx, q, endpoint); err != nil {
		return fmt.Errorf("borrando la suscripcion muerta: %w", err)
	}
	return nil
}

// DeUsuario son los dispositivos de una persona. Puede tener varios: el
// celular y el computador son dos suscripciones distintas.
func (s *Store) DeUsuario(ctx context.Context, usuarioID int64) ([]Suscripcion, error) {
	const q = `
		SELECT endpoint, p256dh, auth
		FROM push_suscripciones
		WHERE usuario_id = $1
		ORDER BY id`

	filas, err := s.db.QueryContext(ctx, q, usuarioID)
	if err != nil {
		return nil, fmt.Errorf("listando suscripciones: %w", err)
	}
	defer filas.Close()

	lista := []Suscripcion{}
	for filas.Next() {
		var sus Suscripcion
		if err := filas.Scan(&sus.Endpoint, &sus.P256dh, &sus.Auth); err != nil {
			return nil, fmt.Errorf("leyendo suscripcion: %w", err)
		}
		lista = append(lista, sus)
	}
	return lista, filas.Err()
}

// Cuantas dice si el usuario tiene algun dispositivo suscrito. La app lo usa
// para pintar el interruptor de avisos en el estado correcto.
func (s *Store) Cuantas(ctx context.Context, usuarioID int64) (int, error) {
	var n int
	err := s.db.QueryRowContext(ctx,
		`SELECT count(*) FROM push_suscripciones WHERE usuario_id = $1`, usuarioID).Scan(&n)
	if err != nil {
		return 0, fmt.Errorf("contando suscripciones: %w", err)
	}
	return n, nil
}

// anotarFallo lleva la cuenta y borra la suscripcion que ya no vale la pena.
func (s *Store) anotarFallo(ctx context.Context, endpoint string) error {
	const q = `
		UPDATE push_suscripciones
		SET fallos = fallos + 1
		WHERE endpoint = $1
		RETURNING fallos`

	var fallos int
	err := s.db.QueryRowContext(ctx, q, endpoint).Scan(&fallos)
	if errors.Is(err, sql.ErrNoRows) {
		return nil // ya no estaba: nada que anotar
	}
	if err != nil {
		return fmt.Errorf("anotando el fallo de push: %w", err)
	}
	if fallos >= MaxFallos {
		return s.BorrarPorEndpoint(ctx, endpoint)
	}
	return nil
}

func (s *Store) anotarEnvio(ctx context.Context, endpoint string) {
	const q = `UPDATE push_suscripciones SET ultimo_envio = now(), fallos = 0 WHERE endpoint = $1`
	if _, err := s.db.ExecContext(ctx, q, endpoint); err != nil {
		// No se propaga: el aviso YA llego al celular del usuario. Fallar la
		// operacion entera por no poder anotar la fecha seria peor.
		slog.Warn("no se pudo anotar el envio de push", "error", err)
	}
}

// --------------------------------------------------------------------------
// El notificador: lo que usan los avisos
// --------------------------------------------------------------------------

// Notificador junta el enviador con la tabla de suscripciones. Es lo unico
// que el paquete de avisos necesita conocer.
type Notificador struct {
	store    *Store
	enviador *Enviador
}

func NuevoNotificador(store *Store, enviador *Enviador) *Notificador {
	return &Notificador{store: store, enviador: enviador}
}

// Habilitado dice si hay a donde mandar. Un notificador nil (sin llaves
// configuradas) responde false sin explotar, para que quien lo use no tenga
// que preguntarlo dos veces.
func (n *Notificador) Habilitado() bool {
	return n != nil && n.enviador != nil
}

// Avisar manda el mensaje a TODOS los dispositivos del usuario.
//
// Devuelve a cuantos llego. Un dispositivo que falla no impide los demas: si
// el celular viejo ya no existe, el nuevo igual tiene que enterarse.
func (n *Notificador) Avisar(ctx context.Context, usuarioID int64, m Mensaje) (int, error) {
	if !n.Habilitado() {
		return 0, nil
	}

	suscripciones, err := n.store.DeUsuario(ctx, usuarioID)
	if err != nil {
		return 0, err
	}
	if len(suscripciones) == 0 {
		return 0, nil
	}

	cuerpo, err := json.Marshal(m)
	if err != nil {
		return 0, fmt.Errorf("armando el mensaje de push: %w", err)
	}

	var enviados int
	var fallos []error

	for _, sus := range suscripciones {
		err := n.enviador.Enviar(sus, cuerpo)
		switch {
		case err == nil:
			enviados++
			n.store.anotarEnvio(ctx, sus.Endpoint)

		case errors.Is(err, ErrSuscripcionMuerta):
			// No es un fallo: el usuario desinstalo la app o limpio el
			// navegador. Se borra y no se vuelve a intentar nunca.
			if err := n.store.BorrarPorEndpoint(ctx, sus.Endpoint); err != nil {
				fallos = append(fallos, err)
			}

		default:
			fallos = append(fallos, err)
			if err := n.store.anotarFallo(ctx, sus.Endpoint); err != nil {
				fallos = append(fallos, err)
			}
		}
	}

	return enviados, errors.Join(fallos...)
}
