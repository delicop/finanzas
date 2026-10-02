import { createContext, useCallback, useContext, useEffect, useMemo, useState } from 'react'
import { authApi, tokenStorage, verComoStorage } from './api'

const AuthContext = createContext(null)

export function AuthProvider({ children }) {
  const [usuario, setUsuario] = useState(null)
  const [cargando, setCargando] = useState(true)

  // Usuario que el administrador está observando, o null en el caso normal
  // (cada quien viendo lo suyo). Arranca leyendo sessionStorage para que un
  // F5 no tumbe la vista a mitad de una revisión.
  const [verComo, setVerComo] = useState(() => verComoStorage.get())

  // Al abrir la app revisamos si el token guardado sigue siendo valido.
  // Si el backend responde 401, apiFetch ya lo borro del localStorage.
  useEffect(() => {
    if (!tokenStorage.get()) {
      setCargando(false)
      return
    }
    authApi
      .me()
      .then(setUsuario)
      .catch(() => setUsuario(null))
      .finally(() => setCargando(false))
  }, [])

  const login = useCallback(async (email, password) => {
    const datos = await authApi.login(email, password)
    tokenStorage.set(datos.token)
    setUsuario(datos.usuario)
  }, [])

  const logout = useCallback(() => {
    tokenStorage.clear()
    // Salir sin limpiar esto dejaría la próxima sesión mandando la cabecera
    // X-Ver-Como de una revisión vieja.
    verComoStorage.clear()
    setVerComo(null)
    setUsuario(null)
  }, [])

  // "No volver a mostrar la guía". Se anota en el servidor y, sin esperar la
  // respuesta, también aquí: si la petición falla (sin señal), lo peor que
  // pasa es que la guía vuelva a salir la próxima vez.
  const marcarGuiaVista = useCallback(() => {
    setUsuario((u) => (u ? { ...u, guia_vista: true } : u))
    authApi.marcarGuiaVista().catch(() => {})
  }, [])

  const observar = useCallback((objetivo) => {
    verComoStorage.set(objetivo)
    setVerComo(objetivo)
  }, [])

  const dejarDeObservar = useCallback(() => {
    verComoStorage.clear()
    setVerComo(null)
  }, [])

  const valor = useMemo(
    () => ({
      usuario,
      cargando,
      login,
      logout,
      // El rol también lo verifica el servidor en cada petición: esto solo
      // decide qué se dibuja.
      esAdmin: usuario?.rol === 'admin',
      verComo,
      // Mirando una cuenta ajena no se puede escribir nada: el backend
      // rechaza cualquier método que no sea GET mientras va la cabecera.
      soloLectura: verComo !== null,
      // Si el plan de la cuenta incluye el asistente. Solo decide qué se
      // dibuja: quien lo impide de verdad es el backend, en cada mensaje.
      conIA: usuario?.ia === true && verComo === null,
      // Si el plan incluye la sección de tiendas. Mientras se observa a un
      // cliente manda el plan de ÉL, que es de quien son las tiendas que se
      // verían: el backend ya responde por esa cuenta, no por la del admin.
      conTiendas: verComo ? verComo.plan_tiendas === true : usuario?.tiendas === true,
      // Si ya vio la guía de bienvenida. Lo dice el servidor (/me), así que
      // vale en cualquier navegador y aparato suyo.
      guiaVista: usuario?.guia_vista === true,
      marcarGuiaVista,
      observar,
      dejarDeObservar,
    }),
    [usuario, cargando, login, logout, verComo, marcarGuiaVista, observar, dejarDeObservar],
  )

  return <AuthContext.Provider value={valor}>{children}</AuthContext.Provider>
}

export function useAuth() {
  const ctx = useContext(AuthContext)
  if (!ctx) throw new Error('useAuth debe usarse dentro de <AuthProvider>')
  return ctx
}
