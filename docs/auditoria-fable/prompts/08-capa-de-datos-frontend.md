# 08 · Una capa de datos para el frontend

Contexto: no hay capa de datos. Cada página repite a mano el patrón
`cargando / error / recargar` (`Clientes.jsx` ~27-47, `Recurrentes.jsx`
~41-57, `Planes.jsx` ~17-30, `Negocio.jsx` ~67-86, `Movimientos.jsx`
~70-91). `categoriasApi.listar()` + `mediosApi.listar()` se piden en cinco
sitios (`Movimientos`, `Dashboard`, `Recurrentes`, `ChatAsistente`,
`RecurrentesPendientes`). No hay refetch al volver a la pestaña ni al
recuperar la red: una PWA reabierta al día siguiente muestra cifras viejas
hasta que el usuario navega. La invalidación entre el chat y las pantallas
se hace con `lib/eventos.js`, un bus global de un solo evento. Categorías,
medios y contrapartes bajan por props cuatro niveles
(Movimientos → ModalAbonos → FormAbono → CubrirFaltante).

Qué hacer:

1. Adopta **TanStack Query** (`@tanstack/react-query`). Es la única
   dependencia nueva; no agregues librerías de UI.
2. Crea `frontend/src/datos/` con un hook por recurso:
   `useCategorias`, `useMedios`, `useMovimientos(filtros)`, `useResumen`,
   `useRecurrentes`, `useNotificaciones`, y los del admin (`useClientes`,
   `usePlanes`, `useNegocio`). Cada uno con su `queryKey` y `staleTime`
   razonable (categorías y medios: minutos; resumen y movimientos: segundos).
3. Mutaciones con `useMutation` que invaliden por clave: crear/editar/borrar
   movimiento invalida `movimientos`, `resumen` y `notificaciones`; abonos
   igual; confirmar recurrente invalida además `recurrentes`.
4. `refetchOnWindowFocus` y `refetchOnReconnect` activos: es lo que arregla
   la PWA con cifras viejas.
5. **Elimina `lib/eventos.js`**: el chat, al confirmar una propuesta,
   invalida las claves con `queryClient.invalidateQueries`.
6. Quita el prop drilling: `CubrirFaltante` y `FormAbono` piden `useMedios()`
   directamente.
7. Al hacer `logout` y al entrar/salir de "ver como", llama a
   `queryClient.clear()`: los datos del usuario observado no pueden quedar en
   caché al volver a los propios.
8. `apiFetch` ya acepta `senal`; pásale el `signal` que TanStack da a cada
   `queryFn`.
9. Migra página por página, empezando por `Dashboard` y `Movimientos`. Cada
   página migrada debe quedar visiblemente más corta.
10. Documenta el patrón en `docs/arquitectura.md` (sección Frontend): dónde
    viven los hooks, cómo se invalida, y por qué no se cachea nada en el
    service worker.

No cambies `lib/api.js` más allá de lo necesario: sigue siendo el único
archivo que hace `fetch`.

Las pruebas manuales confirmaron el síntoma que esto arregla: la lista de
Movimientos y la sección "Por confirmar" no se actualizan después de
guardar (crear un movimiento, crear un recurrente, cambiarle la fecha al
pasado); hay que recargar la página. Esos tres casos son la prueba de
aceptación de este prompt.

Verifica: crear un movimiento en el chat debe verse en Movimientos sin
recargar; crear un recurrente con fecha pasada debe aparecer en "Por
confirmar" al instante; cambiar de pestaña y volver debe refrescar el
resumen.

Commit por página migrada, o uno solo: "Los datos se piden una vez y se refrescan solos al volver a la app".
