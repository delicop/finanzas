package httpx

import "context"

// claveContexto es un tipo propio (no string) a proposito: asi ningun otro
// paquete puede pisar nuestra clave en el context por accidente. Es la forma
// idiomatica en Go de guardar valores en un context.
type claveContexto string

const (
	claveUsuarioID claveContexto = "usuario_id"
	claveRol       claveContexto = "rol"
	claveVerComo   claveContexto = "ver_como"
)

// ConUsuarioID guarda el id del usuario autenticado en el context del request.
// Lo escribe el middleware de auth; lo leen los handlers.
func ConUsuarioID(ctx context.Context, id int64) context.Context {
	return context.WithValue(ctx, claveUsuarioID, id)
}

// UsuarioID devuelve el id del usuario CUYOS DATOS se estan consultando.
//
// Ojo: no siempre es quien hizo la peticion. Cuando un admin esta mirando la
// informacion de otro usuario ("ver como"), aqui viene el id del observado,
// que es justo lo que necesitan los stores para filtrar. Para saber quien
// pidio de verdad, esta Observador.
func UsuarioID(ctx context.Context) (int64, bool) {
	id, ok := ctx.Value(claveUsuarioID).(int64)
	return id, ok
}

// ConRol guarda el rol del usuario autenticado ("admin" o "usuario").
//
// El rol NO viaja en el JWT a proposito: el token dura 24 horas, asi que si
// le quitaras el rol a alguien seguiria siendo admin el resto del dia. Se lee
// de la base en cada request, que es una consulta por id sobre la clave
// primaria y en una app de este tamano no se nota.
func ConRol(ctx context.Context, rol string) context.Context {
	return context.WithValue(ctx, claveRol, rol)
}

func Rol(ctx context.Context) (string, bool) {
	rol, ok := ctx.Value(claveRol).(string)
	return rol, ok
}

// EsAdmin es el atajo que usan el middleware y los handlers.
func EsAdmin(ctx context.Context) bool {
	rol, _ := Rol(ctx)
	return rol == "admin"
}

// ConObservador marca que quien hizo la peticion (un admin) NO es el dueno de
// los datos que se estan leyendo. Guarda el id real del que pregunta.
func ConObservador(ctx context.Context, adminID int64) context.Context {
	return context.WithValue(ctx, claveVerComo, adminID)
}

// Observador devuelve el id del admin que esta mirando datos ajenos.
// ok == false en el caso normal: cada quien viendo lo suyo.
func Observador(ctx context.Context) (int64, bool) {
	id, ok := ctx.Value(claveVerComo).(int64)
	return id, ok
}
