// Cliente HTTP unico de la app. Todo el frontend habla con el backend por aqui.
const BASE_URL = import.meta.env.VITE_API_URL ?? 'http://localhost:8080'

const CLAVE_TOKEN = 'finanzas_token'

export const tokenStorage = {
  get: () => localStorage.getItem(CLAVE_TOKEN),
  set: (token) => localStorage.setItem(CLAVE_TOKEN, token),
  clear: () => localStorage.removeItem(CLAVE_TOKEN),
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
