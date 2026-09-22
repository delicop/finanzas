# 21 · Cuota de facturas por usuario y cabeceras de seguridad

Contexto: `backend/internal/movimientos/facturas.go` (~20) limita el tamaño
por archivo, pero nada impide subir miles de facturas y llenar la tarjeta de
la Raspberry. `DescargarFactura` (`handler.go` ~703-714) sirve contenido
subido por usuarios `inline` sin `X-Content-Type-Options: nosniff` ni
`Content-Security-Policy: sandbox`; `VisorFactura.jsx` (~47) incrusta PDFs
en `<object>` bajo el origen de la app, y el admin los abre "viendo como" un
cliente: un PDF hostil correría en el origen del panel. `frontend/nginx.conf`
no manda ninguna cabecera de seguridad.

Qué hacer:

1. **Cuota**: variable `UPLOADS_MAX_MB_POR_USUARIO` (por defecto 200).
   Antes de guardar, sumar el tamaño de las facturas del usuario. Guarda
   `factura_bytes` en `movimientos` (migración) para no recorrer el disco.
   Al superar: 413 con mensaje claro. Muestra el uso en algún sitio del
   perfil o en el panel de admin por cliente.
2. **Descarga de facturas**: `X-Content-Type-Options: nosniff`,
   `Content-Security-Policy: sandbox`, `Content-Disposition: inline;
   filename="..."` solo para imágenes y PDF (ya se detecta el tipo por
   bytes). `Cache-Control: private, no-store`.
3. **`VisorFactura.jsx`**: el PDF en un `<iframe sandbox>` (sin
   `allow-scripts`) en vez de `<object>`.
4. **nginx.conf**: `X-Content-Type-Options`, `X-Frame-Options: DENY`,
   `Referrer-Policy: strict-origin-when-cross-origin`,
   `Permissions-Policy` restrictiva, y una `Content-Security-Policy` que
   permita `self`, `blob:` (los visores usan blob URLs), la URL de la API y
   nada más. Prueba que la PWA, el push y el chat sigan funcionando con la
   CSP puesta; ajusta antes de apretar más.
5. Test en `facturas_test.go`: la cuota se respeta; y en un test de handler,
   que la descarga traiga las cabeceras.
6. Nota en `docs/decisiones.md` (sección Facturas) y en
   `docs/despliegue.md`.

Commit: "Tope de facturas por cuenta y cabeceras que protegen al navegador".
