// Cliente HTTP unico de la app. Todo el frontend habla con el backend por aqui.
const BASE_URL = import.meta.env.VITE_API_URL ?? 'http://localhost:8080'

const CLAVE_TOKEN = 'finanzas_token'
const CLAVE_VER_COMO = 'finanzas_ver_como'

export const tokenStorage = {
  get: () => localStorage.getItem(CLAVE_TOKEN),
  set: (token) => localStorage.setItem(CLAVE_TOKEN, token),
  clear: () => localStorage.removeItem(CLAVE_TOKEN),
}

// "Ver como": el administrador mirando los datos de otro usuario.
//
// Va en sessionStorage y no en localStorage a propósito: es un modo de
// inspección puntual, no una preferencia. Se sobrevive a un F5 (si no, revisar
// una cuenta ajena sería insoportable) pero se acaba al cerrar la pestaña, así
// que nadie vuelve mañana sin darse cuenta de que sigue mirando a otro.
export const verComoStorage = {
  get: () => {
    const crudo = sessionStorage.getItem(CLAVE_VER_COMO)
    if (!crudo) return null
    try {
      return JSON.parse(crudo)
    } catch {
      // Si alguien lo editó a mano y quedó ilegible, mejor salir del modo
      // que dejar la app a medias mandando una cabecera inválida.
      sessionStorage.removeItem(CLAVE_VER_COMO)
      return null
    }
  },
  set: (usuario) => sessionStorage.setItem(CLAVE_VER_COMO, JSON.stringify(usuario)),
  clear: () => sessionStorage.removeItem(CLAVE_VER_COMO),
}

// Error con el detalle por campo que devuelve el backend (formato {error, campos}).
export class ApiError extends Error {
  constructor(mensaje, status, campos) {
    super(mensaje)
    this.status = status
    this.campos = campos ?? {}
  }
}

function cabeceras(auth) {
  const headers = {}
  if (auth) {
    const token = tokenStorage.get()
    if (token) headers['Authorization'] = `Bearer ${token}`

    // Con esta cabecera el backend responde con los datos del usuario
    // observado en vez de los del admin. Solo la acepta de un admin y solo en
    // peticiones GET: cualquier intento de escribir vuelve como 403.
    const verComo = verComoStorage.get()
    if (verComo) headers['X-Ver-Como'] = String(verComo.id)
  }
  return headers
}

export async function apiFetch(ruta, { metodo = 'GET', body, auth = true } = {}) {
  const headers = cabeceras(auth)
  const esFormData = body instanceof FormData

  // Con FormData NO ponemos Content-Type: el navegador tiene que generarlo
  // con el "boundary" del multipart. Si lo escribimos a mano, el backend
  // no puede separar las partes del cuerpo.
  if (body !== undefined && !esFormData) headers['Content-Type'] = 'application/json'

  let respuesta
  try {
    respuesta = await fetch(`${BASE_URL}${ruta}`, {
      method: metodo,
      headers,
      body: body === undefined ? undefined : esFormData ? body : JSON.stringify(body),
    })
  } catch {
    throw new ApiError('No se pudo conectar con el servidor', 0)
  }

  if (respuesta.status === 204) return null

  const datos = await respuesta.json().catch(() => null)

  if (!respuesta.ok) {
    // 401 = token vencido o invalido: limpiamos para forzar login de nuevo.
    if (respuesta.status === 401) tokenStorage.clear()
    throw new ApiError(datos?.error ?? 'Error inesperado', respuesta.status, datos?.campos)
  }

  return datos
}

// Las facturas NO se pueden poner en un <img src="...">: ese request lo hace
// el navegador solo y no lleva el header Authorization. Por eso las bajamos
// con fetch (que si manda el token) y creamos una URL temporal en memoria.
// Ojo: quien la use debe llamar URL.revokeObjectURL al terminar.
export async function descargarFactura(movimientoId) {
  const respuesta = await fetch(`${BASE_URL}/api/movimientos/${movimientoId}/factura`, {
    headers: cabeceras(true),
  })

  if (!respuesta.ok) {
    if (respuesta.status === 401) tokenStorage.clear()
    throw new ApiError('No se pudo cargar la factura', respuesta.status)
  }

  const blob = await respuesta.blob()
  return { url: URL.createObjectURL(blob), tipo: blob.type }
}

function queryString(params) {
  const q = new URLSearchParams()
  for (const [clave, valor] of Object.entries(params ?? {})) {
    if (valor !== '' && valor !== null && valor !== undefined) q.set(clave, valor)
  }
  const s = q.toString()
  return s ? `?${s}` : ''
}

export const authApi = {
  login: (email, password) =>
    apiFetch('/api/auth/login', { metodo: 'POST', body: { email, password }, auth: false }),
  me: () => apiFetch('/api/auth/me'),
  cambiarPassword: (actual, nueva) =>
    apiFetch('/api/auth/password', { metodo: 'POST', body: { actual, nueva } }),
}

export const categoriasApi = {
  listar: () => apiFetch('/api/categorias'),
  crear: (nombre) => apiFetch('/api/categorias', { metodo: 'POST', body: { nombre } }),
  actualizar: (id, nombre) => apiFetch(`/api/categorias/${id}`, { metodo: 'PUT', body: { nombre } }),
  eliminar: (id) => apiFetch(`/api/categorias/${id}`, { metodo: 'DELETE' }),
}

// Medios de pago/recaudo: efectivo, transferencia, Nequi, etc.
// Es una lista aparte de las categorías: la categoría dice DE QUÉ es la plata,
// el medio dice POR DÓNDE entró o salió.
export const mediosApi = {
  listar: () => apiFetch('/api/medios-pago'),
  crear: (nombre) => apiFetch('/api/medios-pago', { metodo: 'POST', body: { nombre } }),
  actualizar: (id, nombre) =>
    apiFetch(`/api/medios-pago/${id}`, { metodo: 'PUT', body: { nombre } }),
  eliminar: (id) => apiFetch(`/api/medios-pago/${id}`, { metodo: 'DELETE' }),
}

export const movimientosApi = {
  listar: (filtros) => apiFetch(`/api/movimientos${queryString(filtros)}`),
  crear: (datos) => apiFetch('/api/movimientos', { metodo: 'POST', body: datos }),
  actualizar: (id, datos) => apiFetch(`/api/movimientos/${id}`, { metodo: 'PUT', body: datos }),
  eliminar: (id) => apiFetch(`/api/movimientos/${id}`, { metodo: 'DELETE' }),

  // Marca un préstamo como pagado/pendiente sin abrir el formulario.
  // PATCH (no PUT) porque cambia un solo campo: así ningún otro dato
  // del movimiento puede pisarse por accidente.
  // medioCobroID: por dónde te devolvieron el préstamo (0 = sin registrar).
  cambiarEstado: (id, estado, medioCobroID = 0) =>
    apiFetch(`/api/movimientos/${id}/estado`, {
      metodo: 'PATCH',
      body: { estado, medio_cobro_id: medioCobroID },
    }),

  subirFactura: (id, archivo) => {
    const form = new FormData()
    form.append('factura', archivo)
    return apiFetch(`/api/movimientos/${id}/factura`, { metodo: 'POST', body: form })
  },
  eliminarFactura: (id) => apiFetch(`/api/movimientos/${id}/factura`, { metodo: 'DELETE' }),
}

export const dashboardApi = {
  resumen: () => apiFetch('/api/dashboard'),
}

// Panel del administrador. Todas estas rutas devuelven 403 para un usuario
// normal: lo que decide es el rol que el backend lee de la base en cada
// petición, no lo que diga el frontend.
export const adminApi = {
  listarUsuarios: () => apiFetch('/api/admin/usuarios'),
  crearUsuario: (datos) => apiFetch('/api/admin/usuarios', { metodo: 'POST', body: datos }),

  // PATCH con solo los campos que cambian: así desactivar a alguien no puede
  // pisarle el nombre con un valor viejo que traía la pantalla.
  actualizarUsuario: (id, cambios) =>
    apiFetch(`/api/admin/usuarios/${id}`, { metodo: 'PATCH', body: cambios }),

  resetearPassword: (id, nueva) =>
    apiFetch(`/api/admin/usuarios/${id}/password`, { metodo: 'POST', body: { nueva } }),

  // El correo va como confirmación: el backend lo compara con el de la fila y
  // rechaza el borrado si no coincide. Un id en una URL se equivoca fácil, y
  // esto no se puede deshacer.
  eliminarUsuario: (id, email) =>
    apiFetch(`/api/admin/usuarios/${id}${queryString({ email })}`, { metodo: 'DELETE' }),

  asignarPlan: (id, planID) =>
    apiFetch(`/api/admin/usuarios/${id}/plan`, { metodo: 'PUT', body: { plan_id: planID } }),

  errores: (filtros) => apiFetch(`/api/admin/errores${queryString(filtros)}`),
  conteoErrores: () => apiFetch('/api/admin/errores/conteo'),
  marcarError: (id, resuelto) =>
    apiFetch(`/api/admin/errores/${id}`, { metodo: 'PATCH', body: { resuelto } }),
  borrarErroresResueltos: () => apiFetch('/api/admin/errores/resueltos', { metodo: 'DELETE' }),
}

// El lado NEGOCIO: los planes que vendes y quién ya pagó el mes.
//
// Ojo con la confusión fácil: esto no son las finanzas de los clientes (esas
// son movimientosApi/dashboardApi, y cada cliente ve las suyas). Estas son las
// del dueño del servidor.
export const negocioApi = {
  // periodo en AAAA-MM. Si no se manda, el backend usa el mes actual.
  resumen: (periodo) => apiFetch(`/api/admin/negocio${queryString({ periodo })}`),

  listarPlanes: () => apiFetch('/api/admin/planes'),
  crearPlan: (datos) => apiFetch('/api/admin/planes', { metodo: 'POST', body: datos }),
  actualizarPlan: (id, datos) =>
    apiFetch(`/api/admin/planes/${id}`, { metodo: 'PUT', body: datos }),
  eliminarPlan: (id) => apiFetch(`/api/admin/planes/${id}`, { metodo: 'DELETE' }),

  listarPagos: (periodo) => apiFetch(`/api/admin/pagos${queryString({ periodo })}`),
  // monto vacío = el precio de lista del plan. El backend lo lee de la base,
  // no del formulario: así un dedazo no puede descuadrar los ingresos.
  registrarPago: (datos) => apiFetch('/api/admin/pagos', { metodo: 'POST', body: datos }),
  eliminarPago: (id) => apiFetch(`/api/admin/pagos/${id}`, { metodo: 'DELETE' }),
}
