package auth

import "golang.org/x/crypto/bcrypt"

// costoBcrypt = 12.
//
// bcrypt es deliberadamente lento: cada +1 en el costo duplica el tiempo.
// El default de la libreria es 10; 12 da mas margen contra fuerza bruta.
// Ojo en la Raspberry Pi 4: 12 toma ~300-500 ms por login. Es aceptable
// porque hay un solo usuario y un login por sesion. Si lo sientes lento
// en el Pi, bajar a 11 sigue siendo seguro.
const costoBcrypt = 12

func HashPassword(password string) (string, error) {
	hash, err := bcrypt.GenerateFromPassword([]byte(password), costoBcrypt)
	if err != nil {
		return "", err
	}
	return string(hash), nil
}

// VerificarPassword compara la clave en texto plano contra el hash guardado.
// bcrypt hace la comparacion en tiempo constante, asi que no filtra
// informacion por el tiempo que tarda en responder.
func VerificarPassword(hash, password string) bool {
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(password)) == nil
}
