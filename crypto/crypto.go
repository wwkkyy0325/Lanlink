package crypto

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"io"
)

// GenerateGroupKey creates a random 32-byte AES-256 key for group encryption
func GenerateGroupKey() string {
	key := make([]byte, 32)
	rand.Read(key)
	return base64.RawURLEncoding.EncodeToString(key)
}

// Encrypt encrypts plaintext with AES-256-GCM using the given key
func Encrypt(plaintext string, keyB64 string) (string, error) {
	key, err := base64.RawURLEncoding.DecodeString(keyB64)
	if err != nil {
		return "", err
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", err
	}
	ciphertext := gcm.Seal(nonce, nonce, []byte(plaintext), nil)
	return base64.RawURLEncoding.EncodeToString(ciphertext), nil
}

// Decrypt decrypts AES-256-GCM ciphertext with the given key
func Decrypt(cipherB64 string, keyB64 string) (string, error) {
	key, err := base64.RawURLEncoding.DecodeString(keyB64)
	if err != nil {
		return "", err
	}
	ciphertext, err := base64.RawURLEncoding.DecodeString(cipherB64)
	if err != nil {
		return "", err
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	nonceSize := gcm.NonceSize()
	if len(ciphertext) < nonceSize {
		return "", ErrCipherTooShort
	}
	nonce, ciphertext := ciphertext[:nonceSize], ciphertext[nonceSize:]
	plaintext, err := gcm.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return "", err
	}
	return string(plaintext), nil
}

// DeriveKey derives a key from a group code + salt (for deterministic derivation)
func DeriveKey(code string) string {
	h := sha256.Sum256([]byte("lanlink-group:" + code))
	return base64.RawURLEncoding.EncodeToString(h[:32])
}

// KeyFingerprint returns a short fingerprint of a key for display purposes
func KeyFingerprint(keyB64 string) string {
	key, err := base64.RawURLEncoding.DecodeString(keyB64)
	if err != nil {
		return "invalid"
	}
	h := sha256.Sum256(key)
	return hex.EncodeToString(h[:4])
}

// EncryptMessage is a convenience wrapper that encrypts + adds a prefix tag
func EncryptMessage(content string, groupKey string) string {
	enc, err := Encrypt(content, groupKey)
	if err != nil {
		return "[encryption failed]"
	}
	return "🔒" + enc
}

// TryDecryptMessage detects if a message is encrypted and decrypts it
func TryDecryptMessage(content string, groupKey string) string {
	if len(content) < 3 || content[:3] != "🔒" {
		return content // not encrypted
	}
	dec, err := Decrypt(content[3:], groupKey)
	if err != nil {
		return "[decryption failed: " + err.Error() + "]"
	}
	return dec
}

var ErrCipherTooShort = errCipherTooShort{}

type errCipherTooShort struct{}

func (e errCipherTooShort) Error() string { return "ciphertext too short" }

// CreateInvite creates a shareable invite string: "code:base64key"
func CreateInvite(code string, keyB64 string) string {
	return code + ":" + keyB64
}

// ParseInvite parses an invite string into code and key
func ParseInvite(invite string) (code string, keyB64 string, ok bool) {
	if len(invite) < 8 {
		return "", "", false
	}
	for i := 0; i < len(invite); i++ {
		if invite[i] == ':' {
			return invite[:i], invite[i+1:], i >= 4 && i <= 8
		}
	}
	return "", "", false
}
