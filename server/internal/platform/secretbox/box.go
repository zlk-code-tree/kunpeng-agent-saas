package secretbox

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"fmt"
	"io"
)

type Box struct {
	aead cipher.AEAD
}

func New(secret string) (*Box, error) {
	key := sha256.Sum256([]byte(secret))
	block, err := aes.NewCipher(key[:])
	if err != nil {
		return nil, fmt.Errorf("create credential cipher: %w", err)
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("create credential gcm: %w", err)
	}
	return &Box{aead: aead}, nil
}

func (b *Box) Seal(value string) ([]byte, error) {
	nonce := make([]byte, b.aead.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, fmt.Errorf("generate credential nonce: %w", err)
	}
	return b.aead.Seal(nonce, nonce, []byte(value), nil), nil
}

func (b *Box) Open(value []byte) (string, error) {
	nonceSize := b.aead.NonceSize()
	if len(value) < nonceSize {
		return "", fmt.Errorf("encrypted credential is invalid")
	}
	plain, err := b.aead.Open(nil, value[:nonceSize], value[nonceSize:], nil)
	if err != nil {
		return "", fmt.Errorf("decrypt credential: %w", err)
	}
	return string(plain), nil
}
