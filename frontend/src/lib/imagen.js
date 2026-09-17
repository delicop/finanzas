// Achica una foto antes de subirla.
//
// La cámara de un teléfono saca fotos de 4000 px y 4-6 MB: para ver una
// factura sobran 2000 px, y la subida por datos móviles pasa de lenta a
// inmediata. Si algo falla (un formato que el navegador no sabe dibujar, como
// HEIC en Chrome), se sube la original: el backend la acepta igual.
const LADO_MAXIMO = 2000
const CALIDAD = 0.85

export async function reducirImagen(archivo) {
  if (!archivo?.type?.startsWith('image/') || archivo.type === 'image/heic') return archivo

  try {
    // imageOrientation: la foto sale derecha aunque el teléfono la haya
    // tomado de lado (la orientación viene en los metadatos EXIF).
    const imagen = await createImageBitmap(archivo, { imageOrientation: 'from-image' })
    const escala = Math.min(1, LADO_MAXIMO / Math.max(imagen.width, imagen.height))

    // Ya es pequeña y liviana: no vale la pena recomprimirla.
    if (escala === 1 && archivo.size < 1.5 * 1024 * 1024) {
      imagen.close()
      return archivo
    }

    const lienzo = document.createElement('canvas')
    lienzo.width = Math.round(imagen.width * escala)
    lienzo.height = Math.round(imagen.height * escala)
    lienzo.getContext('2d').drawImage(imagen, 0, 0, lienzo.width, lienzo.height)
    imagen.close()

    const blob = await new Promise((resolver) => lienzo.toBlob(resolver, 'image/jpeg', CALIDAD))
    if (!blob || blob.size >= archivo.size) return archivo

    const nombre = archivo.name.replace(/\.[^.]+$/, '') + '.jpg'
    return new File([blob], nombre, { type: 'image/jpeg' })
  } catch {
    return archivo
  }
}
