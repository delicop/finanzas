// Los íconos de la app, dibujados a mano en SVG de trazo.
//
// Sin librería: son quince dibujos y cada uno pesa menos que una línea de
// importación. Salen del color del texto (currentColor), así que la pestaña
// activa, el hover y el modo oscuro los pintan solos, igual que a las letras.
//
// El trazo va en 1.8 sobre una grilla de 24: a 20px en la barra de abajo se
// ve firme sin ponerse pesado junto a las versalitas.
const TRAZOS = {
  // Secciones
  resumen: (
    <>
      <rect x="3" y="3" width="7.5" height="9" rx="1.5" />
      <rect x="13.5" y="3" width="7.5" height="5" rx="1.5" />
      <rect x="13.5" y="11" width="7.5" height="10" rx="1.5" />
      <rect x="3" y="15" width="7.5" height="6" rx="1.5" />
    </>
  ),
  movimientos: (
    <>
      <path d="M7 20V4" />
      <path d="m3 8 4-4 4 4" />
      <path d="M17 4v16" />
      <path d="m13 16 4 4 4-4" />
    </>
  ),
  recurrentes: (
    <>
      <path d="M3 12a9 9 0 0 1 15.5-6.2L21 8" />
      <path d="M21 3v5h-5" />
      <path d="M21 12a9 9 0 0 1-15.5 6.2L3 16" />
      <path d="M3 21v-5h5" />
    </>
  ),
  categorias: (
    <>
      <path d="M12.6 2.6A2 2 0 0 0 11.2 2H4a2 2 0 0 0-2 2v7.2a2 2 0 0 0 .6 1.4l8.7 8.7a2.4 2.4 0 0 0 3.4 0l6.6-6.6a2.4 2.4 0 0 0 0-3.4z" />
      <circle cx="7.5" cy="7.5" r="1.2" />
    </>
  ),
  medios: (
    <>
      <rect x="2" y="5" width="20" height="14" rx="2" />
      <path d="M2 10h20" />
      <path d="M6 15h4" />
    </>
  ),

  // Administración
  negocio: (
    <>
      <path d="M3 3v18h18" />
      <path d="m7 15 4-4 3 3 6-6" />
    </>
  ),
  clientes: (
    <>
      <circle cx="9" cy="8" r="3.5" />
      <path d="M2.5 20a6.5 6.5 0 0 1 13 0" />
      <path d="M16 4.5a3.5 3.5 0 0 1 0 7" />
      <path d="M18.5 14.5A6.5 6.5 0 0 1 21.5 20" />
    </>
  ),
  planes: (
    <>
      <path d="m12 2 9.5 5-9.5 5-9.5-5z" />
      <path d="m2.5 12 9.5 5 9.5-5" />
      <path d="m2.5 17 9.5 5 9.5-5" />
    </>
  ),
  errores: (
    <>
      <path d="M10.3 3.9 1.8 18a2 2 0 0 0 1.7 3h17a2 2 0 0 0 1.7-3L13.7 3.9a2 2 0 0 0-3.4 0z" />
      <path d="M12 9v4" />
      <path d="M12 17h.01" />
    </>
  ),

  // Hacia dónde va la plata: la flecha apunta hacia ti cuando entra y se
  // aleja cuando sale. Así se lee aunque no se distingan el verde y el rojo.
  entra: (
    <>
      <path d="M17 7 7 17" />
      <path d="M17 17H7V7" />
    </>
  ),
  sale: (
    <>
      <path d="M7 17 17 7" />
      <path d="M7 7h10v10" />
    </>
  ),
  traslado: (
    <>
      <path d="M4 8h16" />
      <path d="m16 4 4 4-4 4" />
      <path d="M20 16H4" />
      <path d="m8 12-4 4 4 4" />
    </>
  ),

  // Barra de arriba
  luna: <path d="M21 12.8A9 9 0 1 1 11.2 3a7 7 0 0 0 9.8 9.8z" />,
  sol: (
    <>
      <circle cx="12" cy="12" r="4" />
      <path d="M12 2v2M12 20v2M4.9 4.9l1.4 1.4M17.7 17.7l1.4 1.4M2 12h2M20 12h2M4.9 19.1l1.4-1.4M17.7 6.3l1.4-1.4" />
    </>
  ),
  campana: (
    <>
      <path d="M6 8a6 6 0 0 1 12 0c0 7 3 9 3 9H3s3-2 3-9" />
      <path d="M10.3 21a1.9 1.9 0 0 0 3.4 0" />
    </>
  ),
  llave: (
    <>
      <circle cx="7.5" cy="15.5" r="4.5" />
      <path d="m10.7 12.3 9.8-9.8" />
      <path d="m15.5 7.5 3 3" />
      <path d="m18.5 4.5 2 2" />
    </>
  ),
  instalar: (
    <>
      <rect x="6" y="2" width="12" height="20" rx="2" />
      <path d="M12 7v7" />
      <path d="m9 11 3 3 3-3" />
    </>
  ),
  celularEncendido: (
    <>
      <rect x="6" y="2" width="12" height="20" rx="2" />
      <path d="M12 18h.01" />
      <path d="M2.5 8.5a6 6 0 0 0 0 7M21.5 8.5a6 6 0 0 1 0 7" />
    </>
  ),
  celularApagado: (
    <>
      <rect x="6" y="2" width="12" height="20" rx="2" />
      <path d="M12 18h.01" />
      <path d="M3 3l18 18" />
    </>
  ),
  salir: (
    <>
      <path d="M9 21H5a2 2 0 0 1-2-2V5a2 2 0 0 1 2-2h4" />
      <path d="m16 17 5-5-5-5" />
      <path d="M21 12H9" />
    </>
  ),
}

export default function Icono({ nombre, tamano = 20, className = '' }) {
  return (
    <svg
      className={`icono-svg ${className}`}
      width={tamano}
      height={tamano}
      viewBox="0 0 24 24"
      fill="none"
      stroke="currentColor"
      strokeWidth="1.8"
      strokeLinecap="round"
      strokeLinejoin="round"
      aria-hidden="true"
      focusable="false"
    >
      {TRAZOS[nombre]}
    </svg>
  )
}
