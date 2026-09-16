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
      observar,
      dejarDeObservar,
    }),
    [usuario, cargando, login, logout, verComo, observar, dejarDeObservar],
  )

  return <AuthContext.Provider value={valor}>{children}</AuthContext.Provider>
}

export function useAuth() {
  const ctx = useContext(AuthContext)
  if (!ctx) throw new Error('useAuth debe usarse dentro de <AuthProvider>')
  return ctx
}
