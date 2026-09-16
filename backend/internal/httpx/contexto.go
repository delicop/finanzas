package httpx

import "context"

// claveContexto es un tipo propio (no string) a proposito: asi ningun otro
// paquete puede pisar nuestra clave en el context por accidente. Es la forma
// idiomatica en Go de guardar valores en un context.
type claveContexto string

const claveUsuarioID claveContexto = "usuario_id"

// ConUsuarioID guarda el id del usuario autenticado en el context del request.
// Lo escribe el middleware de auth; lo leen los handlers.
func ConUsuarioID(ctx context.Context, id int64) context.Context {
	return context.WithValue(ctx, claveUsuarioID, id)
}

// UsuarioID devuelve el id del usuario autenticado.
// ok == false significa que la ruta no paso por RequireAuth.
func UsuarioID(ctx context.Context) (int64, bool) {
	id, ok := ctx.Value(claveUsuarioID).(int64)
	return id, ok
}
