package crypto

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"io"
	"strings"

	"github.com/rs/zerolog/log"
)

// SecureToken creates a new random token
func SecureToken() string {
	b := make([]byte, 16)
	if _, err := io.ReadFull(rand.Reader, b); err != nil {
		panic(err.Error()) // rand should never fail
	}
	return removePadding(base64.URLEncoding.EncodeToString(b))
}

func removePadding(token string) string {
	return strings.TrimRight(token, "=")
}

type AESBlockEncrypter struct {
	Key string
}

func (a *AESBlockEncrypter) Encrypt(plaintext string) (encryptedString string, iv string) {
	// The 16-byte initialization vector (IV)
	ivBytes := make([]byte, aes.BlockSize)
	if _, err := io.ReadFull(rand.Reader, ivBytes); err != nil {
		panic(err)
	}

	return a.EncryptWithIV(plaintext, ivBytes)
}

func (a *AESBlockEncrypter) EncryptWithIV(plaintext string, ivBytes []byte) (encryptedString string, iv string) {
	// Create a new AES cipher block using the key
	block, err := aes.NewCipher([]byte(a.Key))
	if err != nil {
		panic(err)
	}

	// Create a new CBC cipher block mode
	mode := cipher.NewCBCEncrypter(block, ivBytes)

	// Pad the plaintext to a multiple of the block size
	paddedPlaintext := make([]byte, len(plaintext))
	copy(paddedPlaintext, plaintext)
	padLength := aes.BlockSize - len(plaintext)%aes.BlockSize
	padding := bytes.Repeat([]byte{byte(padLength)}, padLength)
	paddedPlaintext = append(paddedPlaintext, padding...)

	// Encrypt the padded plaintext using the CBC cipher and IV
	ciphertext := make([]byte, len(paddedPlaintext))
	mode.CryptBlocks(ciphertext, paddedPlaintext)

	// Encode the ciphertext and IV as base64 strings
	ciphertextBase64 := base64.StdEncoding.EncodeToString(ciphertext)
	ivBase64 := base64.StdEncoding.EncodeToString(ivBytes)
	return ciphertextBase64, ivBase64
}

func (a *AESBlockEncrypter) Decrypt(ciphertextBase64 string, ivBase64 string) (decryptedString string) {
	// Decode the ciphertext and IV from base64 strings
	ciphertext, err := base64.StdEncoding.DecodeString(ciphertextBase64)
	if err != nil {
		panic(err)
	}
	// Create a new AES cipher block using the key
	block, err := aes.NewCipher([]byte(a.Key))
	if err != nil {
		panic(err)
	}

	ivBytes, err := base64.StdEncoding.DecodeString(ivBase64)
	if err != nil {
		log.Error().Msg("Failed to decode iv bytes for user")
	}
	// Create a new CBC cipher block mode for decryption
	mode := cipher.NewCBCDecrypter(block, ivBytes)

	// Decrypt the ciphertext using the CBC cipher and IV
	paddedPlaintext := make([]byte, len(ciphertext))
	mode.CryptBlocks(paddedPlaintext, ciphertext)

	// Remove the padding from the plaintext
	padLength := int(paddedPlaintext[len(paddedPlaintext)-1])
	plaintext := paddedPlaintext[:len(paddedPlaintext)-padLength]

	return string(plaintext)
}
