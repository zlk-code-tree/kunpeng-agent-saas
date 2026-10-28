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

// New 对配置密钥进行 SHA-256 派生并创建 AES-GCM 凭据加密器。
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

// Seal 使用随机 nonce 加密并认证明文凭据。
func (b *Box) Seal(value string) ([]byte, error) {
	// 每次加密生成新 nonce 并把它放在密文前部。GCM 同时保证机密性和完整性，
	// nonce 前缀使解密端无需额外数据库字段即可还原。
	nonce := make([]byte, b.aead.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, fmt.Errorf("generate credential nonce: %w", err)
	}
	return b.aead.Seal(nonce, nonce, []byte(value), nil), nil
}

// Open 从密文前缀读取 nonce，校验完整性并解密凭据。
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
