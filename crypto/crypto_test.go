package crypto

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestEncryption(t *testing.T) {

	encrypter := &AESBlockEncrypter{Key: "testkey_testkey_testkey_testkey_"}
	cipherText, iv := encrypter.Encrypt("hello-world")

	plainText := encrypter.Decrypt(cipherText, iv)
	require.Equal(t, "hello-world", plainText)
}
