// Comando para crear el usuario de la app.
//
// Por que un comando y no un endpoint /register: la app es de UN solo usuario.
// Un endpoint publico de registro seria una puerta abierta a que cualquiera
// se cree una cuenta en el servidor. Aqui el usuario se crea una vez, a mano,
// desde dentro del contenedor:
//
//	docker compose exec backend /app/createuser -email tu@correo.com -nombre "Tu Nombre"
//
// La contrasena se pide por teclado para que NO quede en el historial del shell
// ni en los logs de Docker.
package main

import (
	"bufio"
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"golang.org/x/term"

	"finanzas/internal/auth"
	"finanzas/internal/config"
	"finanzas/internal/db"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	email := flag.String("email", "", "correo del usuario")
	nombre := flag.String("nombre", "", "nombre para mostrar")
	flag.Parse()

	if strings.TrimSpace(*email) == "" {
		return errors.New("falta -email")
	}

	password, err := pedirPassword()
	if err != nil {
		return err
	}
	if len(password) < 8 {
		return errors.New("la contrasena debe tener al menos 8 caracteres")
	}
	if len(password) > 72 {
		return errors.New("la contrasena no puede superar 72 caracteres (limite de bcrypt)")
	}

	cfg, err := config.Load()
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	pool, err := db.Open(ctx, cfg.DatabaseURL)
	if err != nil {
		return err
	}
	defer pool.Close()

	if err := db.Migrate(pool); err != nil {
		return err
	}

	store := auth.NewStore(pool)

	hash, err := auth.HashPassword(password)
	if err != nil {
		return err
	}

	usuario, err := store.Crear(ctx, *email, *nombre, hash)
	if errors.Is(err, auth.ErrEmailDuplicado) {
		return fmt.Errorf("ya existe un usuario con el correo %s", *email)
	}
	if err != nil {
		return err
	}

	fmt.Printf("Usuario creado: id=%d email=%s\n", usuario.ID, usuario.Email)
	return nil
}

// pedirPassword lee la clave sin mostrarla en pantalla.
// Si la entrada no es una terminal (por ejemplo un pipe), cae a lectura normal.
func pedirPassword() (string, error) {
	fd := int(os.Stdin.Fd())
	if term.IsTerminal(fd) {
		fmt.Print("Contrasena: ")
		b1, err := term.ReadPassword(fd)
		if err != nil {
			return "", err
		}
		fmt.Print("\nRepite la contrasena: ")
		b2, err := term.ReadPassword(fd)
		if err != nil {
			return "", err
		}
		fmt.Println()

		if string(b1) != string(b2) {
			return "", errors.New("las contrasenas no coinciden")
		}
		return string(b1), nil
	}

	linea, err := bufio.NewReader(os.Stdin).ReadString('\n')
	if err != nil && linea == "" {
		return "", errors.New("no se pudo leer la contrasena")
	}
	return strings.TrimRight(linea, "\r\n"), nil
}
