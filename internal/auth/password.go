package auth

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"strings"

	"golang.org/x/crypto/argon2"
)

// Настройки Argon2id (чем больше память, тем сложнее взломать)
const (
	argonTime    = 2
	argonMemory  = 64 * 1024
	argonThreads = 2
	argonKeyLen  = 32
)

// makeKey превращает пароль + соль в хеш
func makeKey(password string, salt []byte) []byte {
	return argon2.IDKey([]byte(password), salt, argonTime, argonMemory, argonThreads, argonKeyLen)
}

// HashPassword делает хеш для сохранения в базе.
// Результат выглядит так: соль$хеш
func HashPassword(password string) (string, error) {
	// Случайная соль, у каждого пользователя своя
	salt := make([]byte, 16)
	_, err := rand.Read(salt)
	if err != nil {
		return "", err
	}

	key := makeKey(password, salt)

	saltText := base64.RawStdEncoding.EncodeToString(salt)
	keyText := base64.RawStdEncoding.EncodeToString(key)
	return saltText + "$" + keyText, nil
}

// CheckPassword проверяет пароль при входе (понадобится на следующих шагах)
func CheckPassword(password string, saved string) bool {
	parts := strings.Split(saved, "$")
	if len(parts) != 2 {
		return false
	}

	salt, err1 := base64.RawStdEncoding.DecodeString(parts[0])
	savedKey, err2 := base64.RawStdEncoding.DecodeString(parts[1])
	if err1 != nil || err2 != nil {
		return false
	}

	// Считаем хеш из введённого пароля и той же соли, потом сравниваем
	newKey := makeKey(password, salt)
	return subtle.ConstantTimeCompare(newKey, savedKey) == 1
}
