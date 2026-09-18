import { useAvisosEnElCelular } from '../lib/push'

// El interruptor de los avisos que llegan con la app cerrada.
//
// Vive al lado de la campana: la campana es "lo que pasó", esto es "avísame
// aunque no esté mirando". Si el servidor no tiene llaves VAPID o el navegador
// no lo soporta, no se muestra nada — mejor que ofrecer algo que no funciona.
export default function AvisosCelular() {
  const { estado, error, ocupado, encender, apagar } = useAvisosEnElCelular()

  if (estado === 'cargando' || estado === 'no-disponible') return null

  if (estado === 'bloqueado') {
    // Pedir el permiso otra vez no hace absolutamente nada cuando está
    // bloqueado: el navegador ni siquiera muestra el diálogo. Lo único útil
    // es decir dónde se cambia.
    return (
      <button
        className="boton-tema"
        title="Bloqueaste los avisos para este sitio. Se cambia en los ajustes del navegador, en el candado de la barra de direcciones."
        aria-label="Avisos bloqueados"
        onClick={() =>
          alert(
            'Los avisos están bloqueados para este sitio.\n\n' +
              'Para activarlos, toca el candado (o el ícono de ajustes) que hay al lado de la dirección, ' +
              'busca "Notificaciones" y ponlo en Permitir.',
          )
        }
      >
        🔕
      </button>
    )
  }

  const encendido = estado === 'encendido'

  return (
    <button
      className={`boton-tema ${encendido ? 'activo' : ''}`}
      disabled={ocupado}
      onClick={encendido ? apagar : encender}
      title={
        error ||
        (encendido
          ? 'Los avisos llegan a este dispositivo. Toca para apagarlos.'
          : 'Recibir los avisos en este dispositivo, aunque la app esté cerrada.')
      }
      aria-label={encendido ? 'Apagar avisos en este dispositivo' : 'Recibir avisos en este dispositivo'}
    >
      {ocupado ? '…' : encendido ? '📳' : '📴'}
    </button>
  )
}
