import { createContext, useCallback, useContext, useEffect, useMemo, useState } from 'react'
import { authApi, tokenStorage } from './api'

const AuthContext = createContext(null)

export function AuthProvider({ children }) {
  const [usuario, setUsuario] = useState(null)
  const [cargando, setCargando] = useState(true)

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
    setUsuario(null)
  }, [])

  const valor = useMemo(() => ({ usuario, cargando, login, logout }), [usuario, cargando, login, logout])

  return <AuthContext.Provider value={valor}>{children}</AuthContext.Provider>
}

export function useAuth() {
  const ctx = useContext(AuthContext)
  if (!ctx) throw new Error('useAuth debe usarse dentro de <AuthProvider>')
  return ctx
}
