package agente

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
)

type Store struct {
	db *sql.DB
}

func NewStore(db *sql.DB) *Store { return &Store{db: db} }

// Igual que en el resto de la app: TODAS las consultas de este archivo llevan
// usuario_id, y ese id viene del JWT. Aqui no hay una sola ruta por la que un
// usuario pueda leer (ni escribir en) el hilo de otro, aunque conociera el id
// de la conversacion.

// MensajesVisibles es cuantos mensajes devuelve la app al abrir el chat.
// Es mas que los que se le mandan al modelo: leer el historial es gratis,
// mandarselo al proveedor no.
const MensajesVisibles = 100

// Activa devuelve la conversacion abierta del usuario con sus mensajes.
// Si no tiene una (nunca ha escrito, o termino la ultima), devuelve
// ErrNoEncontrada: crearla es decision del handler, no un efecto secundario
// de consultarla.
func (s *Store) Activa(ctx context.Context, usuarioID int64) (*Conversacion, error) {
	// La base garantiza que hay a lo sumo una abierta por usuario
	// (agente_conversaciones_una_abierta).
	const q = `
		SELECT id
		FROM agente_conversaciones
		WHERE usuario_id = $1 AND archivada_en IS NULL`

	var conv Conversacion
	err := s.db.QueryRowContext(ctx, q, usuarioID).Scan(&conv.ID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNoEncontrada
	}
	if err != nil {
		return nil, fmt.Errorf("consultando conversacion activa: %w", err)
	}

	conv.Mensajes, err = s.Historial(ctx, usuarioID, conv.ID, MensajesVisibles)
	if err != nil {
		return nil, err
	}
	return &conv, nil
}

// Crear abre una conversacion vacia y devuelve su id.
func (s *Store) Crear(ctx context.Context, usuarioID int64) (int64, error) {
	const q = `
		INSERT INTO agente_conversaciones (usuario_id)
		VALUES ($1)
		RETURNING id`

	var id int64
	if err := s.db.QueryRowContext(ctx, q, usuarioID).Scan(&id); err != nil {
		// 23505: otra peticion del mismo usuario (otra pestaña) la abrio un
		// instante antes. No es un error: quien llama vuelve a leer la abierta.
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			return 0, ErrYaAbierta
		}
		return 0, fmt.Errorf("creando conversacion: %w", err)
	}
	return id, nil
}

// Historial devuelve los ultimos `limite` mensajes en orden cronologico.
//
// La subconsulta ordena al reves para quedarse con la COLA del hilo (los
// ultimos), y el SELECT de afuera la vuelve a poner en orden para leerla. Con
// un solo ORDER BY id LIMIT n se traeria el principio de la conversacion, que
// es justo lo contrario de lo que se necesita.
func (s *Store) Historial(ctx context.Context, usuarioID, conversacionID int64, limite int) ([]Mensaje, error) {
	const q = `
		SELECT id, rol, contenido, herramientas, creado_en
		FROM (
			SELECT id, rol, contenido, herramientas, creado_en
			FROM agente_mensajes
			WHERE conversacion_id = $1 AND usuario_id = $2
			ORDER BY id DESC
			LIMIT $3
		) ultimos
		ORDER BY id`

	filas, err := s.db.QueryContext(ctx, q, conversacionID, usuarioID, limite)
	if err != nil {
		return nil, fmt.Errorf("leyendo el hilo: %w", err)
	}
	defer filas.Close()

	// Slice inicializado (no nil) para que el JSON sea [] y no null.
	mensajes := []Mensaje{}
	for filas.Next() {
		m, err := escanearMensaje(filas)
		if err != nil {
			return nil, err
		}
		mensajes = append(mensajes, *m)
	}
	if err := filas.Err(); err != nil {
		return nil, fmt.Errorf("recorriendo el hilo: %w", err)
	}
	return mensajes, nil
}

// GuardarMensaje agrega una linea al hilo.
//
// El INSERT sale de un SELECT sobre la conversacion del usuario: si la
// conversacion no es suya, no hay fila que insertar y la consulta no devuelve
// nada. Es el mismo patron que usa movimientos para validar la categoria: una
// sola consulta, sin ventana entre "verifico" e "inserto".
func (s *Store) GuardarMensaje(ctx context.Context, usuarioID, conversacionID int64, nuevo MensajeNuevo) (*Mensaje, error) {
	const q = `
		WITH conv AS (
			SELECT id FROM agente_conversaciones
			WHERE id = $2 AND usuario_id = $1
		),
		toca AS (
			UPDATE agente_conversaciones SET actualizada_en = now()
			WHERE id = (SELECT id FROM conv)
		),
		ins AS (
			INSERT INTO agente_mensajes
				(conversacion_id, usuario_id, rol, contenido, tokens_entrada, tokens_salida, herramientas)
			SELECT conv.id, $1, $3, $4, $5, $6, $7 FROM conv
			RETURNING id, rol, contenido, herramientas, creado_en
		)
		SELECT id, rol, contenido, herramientas, creado_en FROM ins`

	// El arreglo viaja como JSON. Nunca nil: la columna es NOT NULL y un
	// "null" ahi obligaria a revisarlo en cada lectura.
	if nuevo.Herramientas == nil {
		nuevo.Herramientas = []string{}
	}
	herramientas, err := json.Marshal(nuevo.Herramientas)
	if err != nil {
		return nil, fmt.Errorf("serializando las herramientas usadas: %w", err)
	}

	fila := s.db.QueryRowContext(ctx, q, usuarioID, conversacionID,
		nuevo.Rol, nuevo.Contenido, nuevo.TokensEntrada, nuevo.TokensSalida, herramientas)

	m, err := escanearMensaje(fila)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNoEncontrada
	}
	if err != nil {
		return nil, fmt.Errorf("guardando mensaje: %w", err)
	}
	return m, nil
}

// escaneable lo cumplen tanto *sql.Row como *sql.Rows: asi la lectura de un
// mensaje se escribe una sola vez. Es el mismo patron que usa movimientos.
type escaneable interface {
	Scan(dest ...any) error
}

func escanearMensaje(fila escaneable) (*Mensaje, error) {
	var m Mensaje
	var herramientas []byte

	if err := fila.Scan(&m.ID, &m.Rol, &m.Contenido, &herramientas, &m.CreadoEn); err != nil {
		return nil, err
	}
	if len(herramientas) > 0 {
		if err := json.Unmarshal(herramientas, &m.Herramientas); err != nil {
			return nil, fmt.Errorf("leyendo las herramientas del mensaje: %w", err)
		}
	}
	return &m, nil
}

// RegistrarConsumo anota que el usuario gasto un mensaje.
//
// Va en su propia tabla y no se deduce de agente_mensajes porque el historial
// se puede borrar desde la app: si el contador viviera ahi, el limite se
// reiniciaria con el boton de "empezar de cero".
func (s *Store) RegistrarConsumo(ctx context.Context, usuarioID int64) error {
	const q = `INSERT INTO agente_consumo (usuario_id) VALUES ($1)`

	if _, err := s.db.ExecContext(ctx, q, usuarioID); err != nil {
		return fmt.Errorf("registrando consumo: %w", err)
	}
	return nil
}

// MensajesRecientes cuenta lo que gasto el usuario en las ultimas 24 horas,
// que es contra lo que corre el limite.
//
// Es una ventana movil y no "desde la medianoche" a proposito: el servidor
// corre en UTC y el usuario no, asi que el corte a medianoche le llegaria a
// una hora arbitraria de la tarde.
func (s *Store) MensajesRecientes(ctx context.Context, usuarioID int64) (int, error) {
	const q = `
		SELECT count(*)
		FROM agente_consumo
		WHERE usuario_id = $1
		  AND creado_en > now() - interval '24 hours'`

	var n int
	if err := s.db.QueryRowContext(ctx, q, usuarioID).Scan(&n); err != nil {
		return 0, fmt.Errorf("contando mensajes recientes: %w", err)
	}
	return n, nil
}

// LimpiarConsumoViejo borra las marcas que ya salieron de la ventana. La tabla
// crece con cada mensaje y nadie vuelve a mirar las de anteayer.
func (s *Store) LimpiarConsumoViejo(ctx context.Context) (int64, error) {
	const q = `DELETE FROM agente_consumo WHERE creado_en < now() - interval '48 hours'`

	resultado, err := s.db.ExecContext(ctx, q)
	if err != nil {
		return 0, fmt.Errorf("limpiando consumo viejo: %w", err)
	}
	return resultado.RowsAffected()
}

// BorrarTodo elimina las conversaciones del usuario (los mensajes se van por
// CASCADE). Es lo que hace el boton de "empezar de cero".
func (s *Store) BorrarTodo(ctx context.Context, usuarioID int64) error {
	const q = `DELETE FROM agente_conversaciones WHERE usuario_id = $1`

	if _, err := s.db.ExecContext(ctx, q, usuarioID); err != nil {
		return fmt.Errorf("borrando conversaciones: %w", err)
	}
	return nil
}

// NombreUsuario es para saludar por el nombre en las instrucciones del modelo.
//
// Se consulta aqui y no se le pide al paquete auth para no acoplar el chat con
// el login: lo unico que necesita es una columna, y asi este paquete sigue
// dependiendo solo de la base.
func (s *Store) NombreUsuario(ctx context.Context, usuarioID int64) (string, error) {
	const q = `SELECT nombre FROM usuarios WHERE id = $1`

	var nombre string
	err := s.db.QueryRowContext(ctx, q, usuarioID).Scan(&nombre)
	if errors.Is(err, sql.ErrNoRows) {
		// El middleware de auth ya verifico que existe; si llegamos aqui es
		// que lo borraron en medio de la peticion. No es motivo para tumbar
		// el chat: se sigue sin nombre.
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("consultando nombre del usuario: %w", err)
	}
	return nombre, nil
}

// --------------------------------------------------------------------------
// Propuestas: lo que el agente preparo y el usuario todavia no confirma
// --------------------------------------------------------------------------

// GuardarPropuesta deja la propuesta atada a la conversacion del usuario.
//
// Igual que con los mensajes, el INSERT sale de un SELECT sobre la
// conversacion: si no es suya, no hay fila que insertar.
func (s *Store) GuardarPropuesta(ctx context.Context, usuarioID, conversacionID int64, nueva PropuestaNueva) (*Propuesta, error) {
	datos, err := json.Marshal(nueva.Datos)
	if err != nil {
		return nil, fmt.Errorf("serializando la propuesta: %w", err)
	}

	const q = `
		WITH conv AS (
			SELECT id FROM agente_conversaciones
			WHERE id = $2 AND usuario_id = $1
		),
		ins AS (
			INSERT INTO agente_propuestas (usuario_id, conversacion_id, tipo, datos)
			SELECT $1, conv.id, $3, $4 FROM conv
			RETURNING id, tipo, datos, creada_en
		)
		SELECT id, tipo, datos, creada_en FROM ins`

	var p Propuesta
	err = s.db.QueryRowContext(ctx, q, usuarioID, conversacionID, nueva.Tipo, datos).
		Scan(&p.ID, &p.Tipo, &p.Datos, &p.CreadaEn)

	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNoEncontrada
	}
	if err != nil {
		return nil, fmt.Errorf("guardando propuesta: %w", err)
	}
	return &p, nil
}

// PropuestasPendientes son las que el usuario todavia puede confirmar: ni
// resueltas ni caducadas. Las pinta la app al abrir el chat, para que una
// tarjeta sobreviva a recargar la pagina.
func (s *Store) PropuestasPendientes(ctx context.Context, usuarioID int64) ([]Propuesta, error) {
	const q = `
		SELECT id, tipo, datos, creada_en
		FROM agente_propuestas
		WHERE usuario_id = $1
		  AND estado = 'pendiente'
		  AND creada_en > now() - $2::interval
		ORDER BY id`

	filas, err := s.db.QueryContext(ctx, q, usuarioID, VigenciaPropuesta.String())
	if err != nil {
		return nil, fmt.Errorf("listando propuestas: %w", err)
	}
	defer filas.Close()

	propuestas := []Propuesta{}
	for filas.Next() {
		var p Propuesta
		if err := filas.Scan(&p.ID, &p.Tipo, &p.Datos, &p.CreadaEn); err != nil {
			return nil, fmt.Errorf("leyendo propuesta: %w", err)
		}
		propuestas = append(propuestas, p)
	}
	if err := filas.Err(); err != nil {
		return nil, fmt.Errorf("recorriendo propuestas: %w", err)
	}
	return propuestas, nil
}

// EstadoDeLasPropuestas cuenta en que quedo cada tarjeta de esta
// conversacion: cual sigue pendiente, cual confirmo el usuario y cual
// descarto.
//
// Existe por un bug concreto: del ir y venir con las herramientas no queda
// nada en el hilo (ver pasosDe), asi que lo UNICO que el modelo vuelve a leer
// de una tarjeta es la frase con que la anuncio — "te la dejo preparada,
// confirmala ahi". En el turno siguiente esa frase sigue ahi aunque la tarjeta
// ya no: el usuario la guardo, la descarto o caduco. Sin este dato el modelo
// manda a confirmar una tarjeta que no esta en pantalla, que es justo lo que
// el usuario reporto.
func (s *Store) EstadoDeLasPropuestas(ctx context.Context, usuarioID, conversacionID int64) ([]PropuestaConEstado, error) {
	// Las ultimas, no todas: una conversacion larga no puede ir engordando el
	// prompt (y la cuenta del proveedor) tarjeta a tarjeta.
	const q = `
		SELECT id, tipo, datos, creada_en, estado, movimiento_id
		FROM agente_propuestas
		WHERE usuario_id = $1 AND conversacion_id = $2
		ORDER BY id DESC
		LIMIT $3`

	filas, err := s.db.QueryContext(ctx, q, usuarioID, conversacionID, PropuestasDeContexto)
	if err != nil {
		return nil, fmt.Errorf("consultando el estado de las propuestas: %w", err)
	}
	defer filas.Close()

	estado := []PropuestaConEstado{}
	for filas.Next() {
		var p PropuestaConEstado
		var movimientoID sql.NullInt64
		if err := filas.Scan(&p.ID, &p.Tipo, &p.Datos, &p.CreadaEn, &p.Estado, &movimientoID); err != nil {
			return nil, fmt.Errorf("leyendo el estado de una propuesta: %w", err)
		}
		p.Guardada = movimientoID.Valid
		// Caducada es lo mismo que le pasa a la tarjeta en pantalla: sigue
		// 'pendiente' en la base, pero ya no se puede confirmar.
		p.Caducada = p.Estado == EstadoPropuestaPendiente && time.Since(p.CreadaEn) > VigenciaPropuesta
		estado = append(estado, p)
	}
	if err := filas.Err(); err != nil {
		return nil, fmt.Errorf("recorriendo el estado de las propuestas: %w", err)
	}

	// La consulta va al reves (las ultimas primero); el modelo las lee mejor
	// en el orden en que pasaron.
	slices.Reverse(estado)
	return estado, nil
}

// Propuesta devuelve una del usuario, sea cual sea su estado. El estado lo
// interpreta quien llama: no es lo mismo "no existe" que "ya la confirmaste".
func (s *Store) Propuesta(ctx context.Context, usuarioID, id int64) (*Propuesta, string, error) {
	const q = `
		SELECT id, tipo, datos, creada_en, estado
		FROM agente_propuestas
		WHERE id = $1 AND usuario_id = $2`

	var p Propuesta
	var estado string
	err := s.db.QueryRowContext(ctx, q, id, usuarioID).
		Scan(&p.ID, &p.Tipo, &p.Datos, &p.CreadaEn, &estado)

	if errors.Is(err, sql.ErrNoRows) {
		return nil, "", ErrPropuestaNoEncontrada
	}
	if err != nil {
		return nil, "", fmt.Errorf("consultando propuesta: %w", err)
	}
	return &p, estado, nil
}

// ReservarPropuesta la marca como resuelta SOLO si seguia pendiente, y dice si
// gano la carrera.
//
// Aqui esta el freno al doble clic, y esta en la base y no en Go a proposito:
// dos peticiones simultaneas ejecutan este UPDATE, pero solo una encuentra la
// fila en 'pendiente'. La otra recibe ErrPropuestaResuelta y no crea un
// segundo movimiento. Con un SELECT y luego un UPDATE habria una ventana entre
// los dos en la que ambas pasarian.
func (s *Store) ReservarPropuesta(ctx context.Context, usuarioID, id int64, estado string) error {
	const q = `
		UPDATE agente_propuestas
		SET estado = $3, resuelta_en = now()
		WHERE id = $1 AND usuario_id = $2 AND estado = 'pendiente'`

	resultado, err := s.db.ExecContext(ctx, q, id, usuarioID, estado)
	if err != nil {
		return fmt.Errorf("resolviendo propuesta: %w", err)
	}

	filas, err := resultado.RowsAffected()
	if err != nil {
		return fmt.Errorf("verificando la propuesta: %w", err)
	}
	if filas == 0 {
		return ErrPropuestaResuelta
	}
	return nil
}

// DevolverPropuestaAPendiente deshace la reserva cuando la escritura que venia
// despues falla. Sin esto, un error al crear el movimiento dejaria la
// propuesta marcada como confirmada sin que se haya creado nada, y el usuario
// no podria reintentar.
func (s *Store) DevolverPropuestaAPendiente(ctx context.Context, usuarioID, id int64) error {
	const q = `
		UPDATE agente_propuestas
		SET estado = 'pendiente', resuelta_en = NULL
		WHERE id = $1 AND usuario_id = $2`

	if _, err := s.db.ExecContext(ctx, q, id, usuarioID); err != nil {
		return fmt.Errorf("devolviendo la propuesta a pendiente: %w", err)
	}
	return nil
}

// AnotarMovimiento deja escrito que movimiento nacio de esta propuesta.
func (s *Store) AnotarMovimiento(ctx context.Context, usuarioID, id, movimientoID int64) error {
	const q = `
		UPDATE agente_propuestas
		SET movimiento_id = $3
		WHERE id = $1 AND usuario_id = $2`

	if _, err := s.db.ExecContext(ctx, q, id, usuarioID, movimientoID); err != nil {
		return fmt.Errorf("anotando el movimiento de la propuesta: %w", err)
	}
	return nil
}

/* -------------------------- conversaciones guardadas -------------------- */

// LargoTitulo es cuanto de la primera pregunta se guarda como titulo.
const LargoTitulo = 80

// MensajesArchivados es el tope de mensajes que se devuelven al abrir una
// conversacion guardada. Leer es gratis, pero no infinito.
const MensajesArchivados = 500

// Terminar archiva la conversacion abierta: la app vuelve a un chat limpio y
// el modelo deja de leer ese hilo, pero se puede releer y descargar.
//
// Las tarjetas que quedaron sin confirmar en esa conversacion se descartan:
// si no, seguirian apareciendo en el chat nuevo sin el contexto que las
// explica. Una conversacion sin mensajes no se guarda: se borra.
//
// Devuelve false si no habia nada que guardar.
func (s *Store) Terminar(ctx context.Context, usuarioID int64) (bool, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return false, fmt.Errorf("abriendo transaccion: %w", err)
	}
	defer func() { _ = tx.Rollback() }() // tras el Commit no hace nada

	var id int64
	var mensajes int
	err = tx.QueryRowContext(ctx, `
		SELECT c.id, (SELECT count(*) FROM agente_mensajes m WHERE m.conversacion_id = c.id)
		FROM agente_conversaciones c
		WHERE c.usuario_id = $1 AND c.archivada_en IS NULL
		FOR UPDATE`, usuarioID).Scan(&id, &mensajes)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("buscando la conversacion abierta: %w", err)
	}

	if _, err := tx.ExecContext(ctx, `
		UPDATE agente_propuestas
		SET estado = 'descartada', resuelta_en = now()
		WHERE usuario_id = $1 AND conversacion_id = $2 AND estado = 'pendiente'`,
		usuarioID, id); err != nil {
		return false, fmt.Errorf("descartando propuestas: %w", err)
	}

	if mensajes == 0 {
		if _, err := tx.ExecContext(ctx,
			`DELETE FROM agente_conversaciones WHERE id = $1 AND usuario_id = $2`, id, usuarioID); err != nil {
			return false, fmt.Errorf("borrando conversacion vacia: %w", err)
		}
		return false, tx.Commit()
	}

	if _, err := tx.ExecContext(ctx, `
		UPDATE agente_conversaciones c
		SET archivada_en = now(),
		    titulo = coalesce((
		        SELECT left(m.contenido, $3)
		        FROM agente_mensajes m
		        WHERE m.conversacion_id = c.id AND m.rol = 'usuario'
		        ORDER BY m.id
		        LIMIT 1), '')
		WHERE c.id = $1 AND c.usuario_id = $2`, id, usuarioID, LargoTitulo); err != nil {
		return false, fmt.Errorf("archivando conversacion: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return false, fmt.Errorf("confirmando: %w", err)
	}
	return true, nil
}

const columnasGuardada = `c.id, c.titulo, c.creada_en, c.archivada_en,
	(SELECT count(*) FROM agente_mensajes m WHERE m.conversacion_id = c.id)`

// Guardadas lista las conversaciones archivadas, la mas reciente primero.
func (s *Store) Guardadas(ctx context.Context, usuarioID int64) ([]Guardada, error) {
	filas, err := s.db.QueryContext(ctx, `
		SELECT `+columnasGuardada+`
		FROM agente_conversaciones c
		WHERE c.usuario_id = $1 AND c.archivada_en IS NOT NULL
		ORDER BY c.archivada_en DESC, c.id DESC`, usuarioID)
	if err != nil {
		return nil, fmt.Errorf("listando conversaciones guardadas: %w", err)
	}
	defer filas.Close()

	lista := []Guardada{}
	for filas.Next() {
		var g Guardada
		if err := filas.Scan(&g.ID, &g.Titulo, &g.CreadaEn, &g.ArchivadaEn, &g.Mensajes); err != nil {
			return nil, fmt.Errorf("leyendo conversacion guardada: %w", err)
		}
		lista = append(lista, g)
	}
	return lista, filas.Err()
}

// Guardada devuelve una conversacion archivada con sus mensajes.
func (s *Store) Guardada(ctx context.Context, usuarioID, id int64) (*GuardadaConMensajes, error) {
	var g GuardadaConMensajes
	err := s.db.QueryRowContext(ctx, `
		SELECT `+columnasGuardada+`
		FROM agente_conversaciones c
		WHERE c.id = $1 AND c.usuario_id = $2 AND c.archivada_en IS NOT NULL`,
		id, usuarioID).Scan(&g.ID, &g.Titulo, &g.CreadaEn, &g.ArchivadaEn, &g.Mensajes)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNoEncontrada
	}
	if err != nil {
		return nil, fmt.Errorf("consultando conversacion guardada: %w", err)
	}

	g.Hilo, err = s.Historial(ctx, usuarioID, id, MensajesArchivados)
	if err != nil {
		return nil, err
	}
	return &g, nil
}

// BorrarGuardada elimina una conversacion archivada. La abierta no se toca
// por aqui: para esa esta Terminar.
func (s *Store) BorrarGuardada(ctx context.Context, usuarioID, id int64) error {
	res, err := s.db.ExecContext(ctx, `
		DELETE FROM agente_conversaciones
		WHERE id = $1 AND usuario_id = $2 AND archivada_en IS NOT NULL`, id, usuarioID)
	if err != nil {
		return fmt.Errorf("borrando conversacion guardada: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("verificando borrado: %w", err)
	}
	if n == 0 {
		return ErrNoEncontrada
	}
	return nil
}
