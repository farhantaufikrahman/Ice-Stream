package auth

import "golang.org/x/crypto/bcrypt"

// bcryptCost 12 butuh sekitar 200-300 ms per hash. Cukup lambat untuk
// mempersulit tebak-tebakan password, tapi masih nyaman untuk login.
// Berupa var (bukan const) supaya unit test bisa memakai nilai kecil.
var bcryptCost = 12

// HashPassword mengubah password menjadi hash bcrypt untuk disimpan.
func HashPassword(password string) (string, error) {
	b, err := bcrypt.GenerateFromPassword([]byte(password), bcryptCost)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

// CheckPassword mengembalikan true jika password cocok dengan hash.
func CheckPassword(hash, password string) bool {
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(password)) == nil
}
