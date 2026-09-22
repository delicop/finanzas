# 15 · Carga perezosa por ruta

Contexto: `frontend/src/App.jsx` (~5-14) importa las diez páginas de forma
estática y no hay ningún `lazy(` en `src/`. El bundle es ~411 KB de JS
(~120 KB gzip) y un cliente normal descarga `Negocio`, `Clientes`, `Planes`,
`Errores` y el chat completo aunque `conIA` sea falso.

Qué hacer:

1. `React.lazy` + `Suspense` para cada página en `App.jsx`, con un fallback
   discreto (el mismo esqueleto o spinner que use la app).
2. `BurbujaAsistente`/`ChatAsistente` también perezosos, y solo montados si
   `conIA` es true (`Layout.jsx` ~203).
3. En `vite.config.js`, `build.rollupOptions.output.manualChunks` para
   separar `react`, `react-dom` y `react-router-dom` en un chunk `vendor`
   (y `@tanstack/react-query` si ya está por el prompt 08).
4. Comprueba que `sw.js` sigue cacheando `/assets/` por hash: los chunks
   nuevos entran solos.
5. Mide antes y después con `npm run build` y anota los tamaños en el
   mensaje del commit.

Commit: "La app carga solo la pantalla que se abre, no todas a la vez".
