import { useEffect, useState } from 'react'
import { adminApi } from '../lib/api'

// La bitácora del servidor: los errores 500 y los pánicos que quedaron
// guardados en la base.
//
// Por qué en la base y no solo en los logs de Docker: la app vive en una
// Raspberry. Entrar por SSH a leer `docker compose logs` desde el celular no es
// práctico, y los logs se pierden al reconstruir la imagen.
export default function Errores() {
  const [errores, setErrores] = useState([])
  const [total, setTotal] = useState(0)
  const [pendientes, setPendientes] = useState(0)
  const [soloPendientes, setSoloPendientes] = useState(true)
  const [cargando, setCargando] = useState(true)
  const [fallo, setFallo] = useState('')
  const [abierto, setAbierto] = useState(null) // id del error con la traza abierta

  async function recargar(pendientesSolo = soloPendientes) {
    setFallo('')
    try {
      const [datos, conteo] = await Promise.all([
        adminApi.errores({ pendientes: pendientesSolo ? 1 : '', limite: 50 }),
        adminApi.conteoErrores(),
      ])
      setErrores(datos.errores)
      setTotal(datos.total)
      setPendientes(conteo.pendientes)
    } catch (err) {
      setFallo(err.message)
    } finally {
      setCargando(false)
    }
  }

  useEffect(() => {
    recargar()
  }, [])

  async function marcar(e, resuelto) {
    try {
      await adminApi.marcarError(e.id, resuelto)
      await recargar()
    } catch (err) {
      setFallo(err.message)
    }
  }

  async function limpiar() {
    if (!confirm('¿Borrar del historial todos los errores ya marcados como resueltos?')) return
    try {
      await adminApi.borrarErroresResueltos()
      await recargar()
    } catch (err) {
      setFallo(err.message)
    }
  }

  function cambiarFiltro(valor) {
    setSoloPendientes(valor)
    setCargando(true)
    recargar(valor)
  }

  return (
    <>
      <div className="encabezado-pagina">
        <div>
          <span className="kicker">Bitácora</span>
          <h1>Errores del servidor</h1>
        </div>
        <button className="secundario" onClick={limpiar}>
          Limpiar resueltos
        </button>
      </div>

      {fallo && <div className="alerta">{fallo}</div>}

      <section className="tarjeta">
        <div className="grupo-tipos">
          <button
            type="button"
            className={`chip ${soloPendientes ? 'chip-activo' : ''}`}
            onClick={() => cambiarFiltro(true)}
          >
            Sin revisar ({pendientes})
          </button>
          <button
            type="button"
            className={`chip ${soloPendientes ? '' : 'chip-activo'}`}
            onClick={() => cambiarFiltro(false)}
          >
            Todos
          </button>
        </div>

        {cargando ? (
          <p className="tenue">Cargando...</p>
        ) : errores.length === 0 ? (
          <p className="tenue">
            {soloPendientes
              ? 'Ningún error pendiente. El servidor está limpio.'
              : 'No hay errores registrados.'}
          </p>
        ) : (
          <>
            <p className="tenue sub">
              {errores.length} de {total}
            </p>
            <div className="lista-movil">
              {errores.map((e) => (
                <article className="tarjeta-cat" key={e.id}>
                  <div className="tarjeta-cat-arriba">
                    <strong>
                      {e.es_panico && <span className="etiqueta tipo-pague">Pánico</span>}{' '}
                      {e.contexto}
                    </strong>
                    <span className="tenue nowrap">
                      {new Date(e.ocurrido_en).toLocaleString()}
                    </span>
                  </div>

                  <div className="tenue sub">
                    {e.metodo} {e.ruta}
                    {/* El request_id es lo que permite cruzar esta fila con la
                        línea correspondiente en los logs de Docker. */}
                    {e.request_id && <> · {e.request_id}</>}
                  </div>

                  <pre className="traza-error">
                    {abierto === e.id && e.traza ? e.traza : e.mensaje}
                  </pre>

                  <div className="tarjeta-mov-acciones">
                    {e.traza && (
                      <button
                        className="secundario"
                        onClick={() => setAbierto(abierto === e.id ? null : e.id)}
                      >
                        {abierto === e.id ? 'Ocultar traza' : 'Ver traza'}
                      </button>
                    )}
                    <button className="secundario" onClick={() => marcar(e, !e.resuelto)}>
                      {e.resuelto ? 'Reabrir' : 'Marcar resuelto'}
                    </button>
                  </div>
                </article>
              ))}
            </div>
          </>
        )}
      </section>
    </>
  )
}
