// Comando vapid: genera el par de llaves para las notificaciones push.
//
// Se corre UNA vez y las dos lineas que imprime se pegan en el .env:
//
//	go run ./cmd/vapid
//
// Si algun dia se cambian las llaves, todos los navegadores suscritos con las
// viejas dejan de recibir avisos y tienen que volver a activarlos. No es una
// tragedia, pero tampoco es algo que se haga sin motivo.
package main

import (
	"fmt"
	"os"

	"finanzas/internal/push"
)

func main() {
	publica, privada, err := push.GenerarLlaves()
	if err != nil {
		fmt.Fprintln(os.Stderr, "no se pudieron generar las llaves:", err)
		os.Exit(1)
	}

	fmt.Println("# Pega estas tres lineas en tu .env")
	fmt.Println("PUSH_VAPID_PUBLIC=" + publica)
	fmt.Println("PUSH_VAPID_PRIVATE=" + privada)
	fmt.Println("PUSH_CONTACTO=mailto:tu@correo.com")
	fmt.Println()
	fmt.Println("# La publica tambien la ve el navegador (no es secreta).")
	fmt.Println("# La privada NO se comparte ni se sube al repositorio.")
}
