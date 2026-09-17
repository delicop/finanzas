// guardarArchivo ofrece un archivo generado en memoria (Blob) como descarga,
// con el nombre dado. Es la única forma de bajar algo que pide el token: un
// <a href> directo al backend no mandaría el header Authorization.
export function guardarArchivo(blob, nombre) {
  const url = URL.createObjectURL(blob)
  const enlace = document.createElement('a')
  enlace.href = url
  enlace.download = nombre
  document.body.appendChild(enlace)
  enlace.click()
  enlace.remove()
  // Un respiro antes de soltar la URL: algunos navegadores empiezan la
  // descarga después del clic.
  setTimeout(() => URL.revokeObjectURL(url), 1000)
}
