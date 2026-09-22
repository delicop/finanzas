# 35 · La búsqueda no ignora tildes y "%" devuelve todo

Contexto: `movimientos/store.go` (~161-169) busca con
`ILIKE '%' || texto || '%'`. Dos problemas: "papa" no encuentra "Papá", y
un texto con `%` o `_` se interpreta como comodín de LIKE (con "%" sale
la lista entera). No es inyección (el texto va como parámetro), pero es un
resultado equivocado.

Qué hacer:

1. Migración: `CREATE EXTENSION IF NOT EXISTS unaccent;` (viene con
   `postgres:16-alpine`) y una función inmutable
   `sin_tildes(text)` que envuelva `unaccent` (la de la extensión no es
   inmutable y no sirve para índices).
2. Consulta: `sin_tildes(m.descripcion) ILIKE sin_tildes($n)` y lo mismo en
   `a_quien`. Escapa `%`, `_` y `\` del texto del usuario en Go antes de
   armar el patrón, y agrega `ESCAPE '\'` a la cláusula.
3. Índice `gin (sin_tildes(descripcion) gin_trgm_ops)` solo si la lista se
   siente lenta; a la escala actual no hace falta. Déjalo anotado.
4. Aplica el mismo helper a cualquier otro buscador del backend (clientes
   en `admin/store.go`, conversaciones guardadas del agente).
5. Extiende `TestBusquedaNoEsVulnerableAInyeccion` con dos casos: "papa"
   encuentra "Papá", y "%" no devuelve nada que no contenga "%".
6. Nota en `docs/api.md` (Movimientos, parámetro `q`).

Commit: "Buscar 'papa' encuentra 'Papá', y el % ya no trae toda la lista".
