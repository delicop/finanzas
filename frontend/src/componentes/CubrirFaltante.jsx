import { formatearMonto } from '../lib/formato'

// Lo que se pregunta cuando el medio no alcanza: "tienes 400.000 y esto es de
// 600.000, ¿de dónde salieron los otros 200.000?".
//
// Ningún medio puede quedar en negativo (ver backend/internal/movimientos/
// fondos.go): esa plata salió de algún lado, y si no se anota de cuál, el
// saldo deja de parecerse a lo que la persona tiene de verdad. Las tres
// respuestas posibles son las tres formas en que entra plata a un medio.
//
// Es controlado: el estado lo tiene quien lo usa, que es quien lo manda al
// backend como `cubrir` junto con el movimiento. El monto no se manda: lo
// calcula el servidor al guardar, para que el medio quede justo en cero.
//
// Si hay plata en los otros medios, lo primero que se ofrece es usarla: con
// 400.000 en el efectivo y un pago de 600.000 por Nequi, lo que te prestaron
// fueron 200.000, no 600.000.
export const CUBRIR_VACIO = {
  usar_otros: true,
  tipo: 'me_prestaron',
  a_quien: '',
  cobrar_el: '',
  categoria_id: '',
  origen_id: '',
}

// En centavos, que son enteros: comparar "400000.00" con "600000.00" como
// decimales es buscarse un error de coma flotante.
const centavos = (monto) => Math.round(Number(monto) * 100)

// Lo que sigue faltando después de usar los otros medios (0 si alcanza).
function restante(c, falta) {
  if (!c.usar_otros || !falta.en_otros?.length) return centavos(falta.falta)
  return Math.max(centavos(falta.falta) - centavos(falta.otros_total), 0)
}

const OPCIONES = [
  ['me_prestaron', 'Me los prestaron'],
  ['recibi', 'Fue un ingreso'],
  ['traslado', 'Los pasé de otro medio'],
]

// cuerpoCubrir es lo que viaja a la API: solo lo que aplica a la opción elegida.
export function cuerpoCubrir(c, falta) {
  const usarOtros = Boolean(c.usar_otros && falta?.en_otros?.length)
  // Si con lo de los otros medios alcanza, no hay nada más que decir.
  if (usarOtros && restante(c, falta) === 0) return { usar_otros: true, tipo: '' }
  return {
    usar_otros: usarOtros,
    tipo: c.tipo,
    a_quien: c.tipo === 'me_prestaron' ? c.a_quien : '',
    cobrar_el: c.tipo === 'me_prestaron' ? c.cobrar_el : '',
    categoria_id: c.tipo === 'recibi' ? Number(c.categoria_id) || 0 : 0,
    origen_id: c.tipo === 'traslado' ? Number(c.origen_id) || 0 : 0,
  }
}

export default function CubrirFaltante({
  falta,
  valor,
  onCambio,
  categorias,
  medios,
  contrapartes = [],
  fecha,
  campos = {},
  // Para que los id no choquen cuando hay varias tarjetas en el chat.
  prefijo = 'cubrir',
}) {
  function cambiar(campo, v) {
    onCambio({ ...valor, [campo]: v })
  }

  const otrosMedios = medios.filter((m) => String(m.id) !== String(falta.medio_id))
  const enRojo = Number(falta.disponible) < 0
  const hayEnOtros = (falta.en_otros?.length ?? 0) > 0
  const usandoOtros = hayEnOtros && valor.usar_otros
  const sigueFaltando = restante(valor, falta)
  // Con los otros medios en uso, "lo pasé de otro medio" ya está dicho.
  const opciones = usandoOtros ? OPCIONES.filter(([v]) => v !== 'traslado') : OPCIONES

  function usarOtros(si) {
    onCambio({ ...valor, usar_otros: si, tipo: si && valor.tipo === 'traslado' ? 'me_prestaron' : valor.tipo })
  }

  return (
    <div className="bloque-prestamo cubrir-faltante" role="alert">
      <p className="cubrir-faltante-texto">
        <strong>No te alcanza.</strong> En {falta.medio}{' '}
        {enRojo ? 'ya estás en ' : 'tienes '}
        <strong>{formatearMonto(falta.disponible)}</strong> y esto es de{' '}
        <strong>{formatearMonto(falta.monto)}</strong>. Faltan{' '}
        <strong>{formatearMonto(falta.falta)}</strong>.
      </p>

      {hayEnOtros && (
        <label className="checkbox">
          <input type="checkbox" checked={valor.usar_otros} onChange={(e) => usarOtros(e.target.checked)} />
          Usar primero lo que tengo en{' '}
          {falta.en_otros.map((o) => `${o.medio} (${formatearMonto(o.saldo)})`).join(', ')}
        </label>
      )}

      {sigueFaltando === 0 ? (
        <p className="ayuda-campo">
          Con eso alcanza: se pasan {formatearMonto(falta.falta)} a {falta.medio} y no debes nada.
        </p>
      ) : (
        <>
          <p className="cubrir-faltante-texto">
            {usandoOtros ? 'Aun así faltan ' : 'Faltan '}
            <strong>{formatearMonto(sigueFaltando / 100)}</strong>: ¿de dónde salieron?
          </p>

          <div className="grupo-tipos">
            {opciones.map(([v, etiqueta]) => (
              <button
                key={v}
                type="button"
                className={`chip ${valor.tipo === v ? 'chip-activo' : ''}`}
                onClick={() => cambiar('tipo', v)}
              >
                {etiqueta}
              </button>
            ))}
          </div>
          {campos['cubrir.tipo'] && <span className="error-campo">{campos['cubrir.tipo']}</span>}

          {valor.tipo === 'me_prestaron' && (
            <>
              <label htmlFor={`${prefijo}-a-quien`}>
                ¿Quién te prestó? <span className="req">*</span>
              </label>
              <input
                id={`${prefijo}-a-quien`}
                list={`${prefijo}-contrapartes`}
                value={valor.a_quien}
                onChange={(e) => cambiar('a_quien', e.target.value)}
                placeholder="Nombre de la persona o del negocio"
              />
              <datalist id={`${prefijo}-contrapartes`}>
                {contrapartes.map((nombre) => (
                  <option key={nombre} value={nombre} />
                ))}
              </datalist>
              {campos['cubrir.a_quien'] && <span className="error-campo">{campos['cubrir.a_quien']}</span>}

              <label htmlFor={`${prefijo}-cobrar-el`}>
                ¿Cuándo le pagas? <span className="tenue">(opcional)</span>
              </label>
              <input
                id={`${prefijo}-cobrar-el`}
                type="date"
                value={valor.cobrar_el}
                min={fecha || undefined}
                onChange={(e) => cambiar('cobrar_el', e.target.value)}
              />
              {campos['cubrir.cobrar_el'] && <span className="error-campo">{campos['cubrir.cobrar_el']}</span>}
              <span className="ayuda-campo tenue">Queda como una deuda tuya, en «Lo que debes».</span>
            </>
          )}

          {valor.tipo === 'recibi' && (
            <>
              <label htmlFor={`${prefijo}-categoria`}>
                ¿Por qué categoría entró? <span className="req">*</span>
              </label>
              <select
                id={`${prefijo}-categoria`}
                value={valor.categoria_id}
                onChange={(e) => cambiar('categoria_id', e.target.value)}
              >
                <option value="">Selecciona una categoría</option>
                {categorias.map((c) => (
                  <option key={c.id} value={c.id}>
                    {c.nombre}
                  </option>
                ))}
              </select>
              {campos['cubrir.categoria_id'] && (
                <span className="error-campo">{campos['cubrir.categoria_id']}</span>
              )}
            </>
          )}

          {valor.tipo === 'traslado' && (
            <>
              <label htmlFor={`${prefijo}-origen`}>
                ¿De qué medio los pasaste a {falta.medio}? <span className="req">*</span>
              </label>
              <select
                id={`${prefijo}-origen`}
                value={valor.origen_id}
                onChange={(e) => cambiar('origen_id', e.target.value)}
              >
                <option value="">Selecciona un medio</option>
                {otrosMedios.map((m) => (
                  <option key={m.id} value={m.id}>
                    {m.nombre}
                  </option>
                ))}
              </select>
              {campos['cubrir.origen_id'] && <span className="error-campo">{campos['cubrir.origen_id']}</span>}
            </>
          )}
        </>
      )}

      <span className="ayuda-campo tenue">
        Al guardar se registra todo junto, y {falta.medio} queda en cero.
      </span>
    </div>
  )
}
