package docker

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"

	"golang.org/x/crypto/ssh"
)

func GenerateSSHKeyPair(bits int) (privateKeyPEM string, publicKey string, err error) {
	// 1. 개인키 생성
	privateKey, err := rsa.GenerateKey(rand.Reader, bits)
	if err != nil {
		return "", "", err
	}

	// 2. PEM 형식으로 인코딩된 개인키
	privDER := x509.MarshalPKCS1PrivateKey(privateKey)
	privBlock := pem.Block{
		Type:  "RSA PRIVATE KEY",
		Bytes: privDER,
	}
	privateKeyPEM = string(pem.EncodeToMemory(&privBlock))

	// 3. SSH 공개키 생성
	pub, err := ssh.NewPublicKey(&privateKey.PublicKey)
	if err != nil {
		return "", "", err
	}
	publicKey = string(ssh.MarshalAuthorizedKey(pub)) // id_rsa.pub 형태

	return privateKeyPEM, publicKey, nil
}
