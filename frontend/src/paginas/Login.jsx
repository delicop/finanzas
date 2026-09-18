import { useState } from 'react'
import { useAuth } from '../lib/AuthContext'
import { ApiError } from '../lib/api'

export default function Login() {
  const { login } = useAuth()
  const [email, setEmail] = useState('')
  const [password, setPassword] = useState('')
  const [error, setError] = useState('')
  const [campos, setCampos] = useState({})
  const [enviando, setEnviando] = useState(false)

  async function onSubmit(e) {
    e.preventDefault()
    setError('')
    setCampos({})
    setEnviando(true)
    try {
      await login(email, password)
    } catch (err) {
      if (err instanceof ApiError) {
        setError(err.message)
        setCampos(err.campos)
      } else {
        setError('Error inesperado')
      }
    } finally {
      setEnviando(false)
    }
  }

  return (
    <div className="pantalla-centrada">
      <form className="tarjeta" onSubmit={onSubmit} noValidate>
        {/* El rótulo dice qué es la app antes que el nombre: quien llega aquí
            por un enlace no tiene por qué saber qué es "Finanzas". */}
        <span className="kicker">Tus gastos, ingresos y préstamos</span>
        <h1>Finanzas</h1>

        {error && <div className="alerta">{error}</div>}

        <label htmlFor="email">Correo</label>
        <input
          id="email"
          type="email"
          autoComplete="username"
          value={email}
          onChange={(e) => setEmail(e.target.value)}
          required
        />
        {campos.email && <span className="error-campo">{campos.email}</span>}

        <label htmlFor="password">Contraseña</label>
        <input
          id="password"
          type="password"
          autoComplete="current-password"
          value={password}
          onChange={(e) => setPassword(e.target.value)}
          required
        />
        {campos.password && <span className="error-campo">{campos.password}</span>}

        <button type="submit" disabled={enviando}>
          {enviando ? 'Entrando...' : 'Entrar'}
        </button>
      </form>
    </div>
  )
}
