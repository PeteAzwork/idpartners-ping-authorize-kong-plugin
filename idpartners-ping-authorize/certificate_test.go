package main

import (
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/pem"
	"math/big"
	"testing"
	"time"
)

func generateSelfSignedCert(key interface{}, pub interface{}) (string, error) {
	template := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "test"},
		NotBefore:    time.Now(),
		NotAfter:     time.Now().Add(time.Hour),
	}

	derBytes, err := x509.CreateCertificate(rand.Reader, template, template, pub, key)
	if err != nil {
		return "", err
	}

	pemBytes := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: derBytes})
	return string(pemBytes), nil
}

func TestExtractClientCertJWK_RSA(t *testing.T) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}

	pemData, err := generateSelfSignedCert(key, &key.PublicKey)
	if err != nil {
		t.Fatal(err)
	}

	jwk, err := ExtractClientCertJWK(pemData, false)
	if err != nil {
		t.Fatal(err)
	}

	if jwk.Kty != "RSA" {
		t.Errorf("expected kty=RSA, got %s", jwk.Kty)
	}
	if jwk.N == "" {
		t.Error("expected non-empty N")
	}
	if jwk.E == "" {
		t.Error("expected non-empty E")
	}
	if len(jwk.X5C) != 1 {
		t.Errorf("expected 1 x5c entry, got %d", len(jwk.X5C))
	}

	// Verify x5c is valid base64 (standard, not URL)
	_, err = base64.StdEncoding.DecodeString(jwk.X5C[0])
	if err != nil {
		t.Errorf("x5c is not valid standard base64: %v", err)
	}
}

func TestExtractClientCertJWK_EC_P256(t *testing.T) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}

	pemData, err := generateSelfSignedCert(key, &key.PublicKey)
	if err != nil {
		t.Fatal(err)
	}

	jwk, err := ExtractClientCertJWK(pemData, false)
	if err != nil {
		t.Fatal(err)
	}

	if jwk.Kty != "EC" {
		t.Errorf("expected kty=EC, got %s", jwk.Kty)
	}
	if jwk.Crv != "P-256" {
		t.Errorf("expected crv=P-256, got %s", jwk.Crv)
	}
	if jwk.X == "" || jwk.Y == "" {
		t.Error("expected non-empty X and Y")
	}
}

func TestExtractClientCertJWK_EC_P384(t *testing.T) {
	key, err := ecdsa.GenerateKey(elliptic.P384(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}

	pemData, err := generateSelfSignedCert(key, &key.PublicKey)
	if err != nil {
		t.Fatal(err)
	}

	jwk, err := ExtractClientCertJWK(pemData, false)
	if err != nil {
		t.Fatal(err)
	}

	if jwk.Crv != "P-384" {
		t.Errorf("expected crv=P-384, got %s", jwk.Crv)
	}
}

func TestExtractClientCertJWK_Ed25519(t *testing.T) {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}

	pemData, err := generateSelfSignedCert(priv, pub)
	if err != nil {
		t.Fatal(err)
	}

	jwk, err := ExtractClientCertJWK(pemData, false)
	if err != nil {
		t.Fatal(err)
	}

	if jwk.Kty != "OKP" {
		t.Errorf("expected kty=OKP, got %s", jwk.Kty)
	}
	if jwk.Crv != "Ed25519" {
		t.Errorf("expected crv=Ed25519, got %s", jwk.Crv)
	}
	if jwk.X == "" {
		t.Error("expected non-empty X")
	}
}

func TestExtractClientCertJWK_FullChain(t *testing.T) {
	// Generate two RSA certs and concatenate them as a "chain"
	key1, _ := rsa.GenerateKey(rand.Reader, 2048)
	key2, _ := rsa.GenerateKey(rand.Reader, 2048)

	pem1, _ := generateSelfSignedCert(key1, &key1.PublicKey)
	pem2, _ := generateSelfSignedCert(key2, &key2.PublicKey)

	fullChain := pem1 + pem2

	jwk, err := ExtractClientCertJWK(fullChain, true)
	if err != nil {
		t.Fatal(err)
	}

	if len(jwk.X5C) != 2 {
		t.Errorf("expected 2 x5c entries for full chain, got %d", len(jwk.X5C))
	}
}

func TestExtractClientCertJWK_LeafOnly(t *testing.T) {
	key1, _ := rsa.GenerateKey(rand.Reader, 2048)
	key2, _ := rsa.GenerateKey(rand.Reader, 2048)

	pem1, _ := generateSelfSignedCert(key1, &key1.PublicKey)
	pem2, _ := generateSelfSignedCert(key2, &key2.PublicKey)

	fullChain := pem1 + pem2

	jwk, err := ExtractClientCertJWK(fullChain, false)
	if err != nil {
		t.Fatal(err)
	}

	if len(jwk.X5C) != 1 {
		t.Errorf("expected 1 x5c entry for leaf only, got %d", len(jwk.X5C))
	}
}

func TestExtractClientCertJWK_NoPEM(t *testing.T) {
	_, err := ExtractClientCertJWK("not a pem", false)
	if err == nil {
		t.Fatal("expected error for invalid PEM")
	}
}

func TestExtractClientCertJWK_EmptyPEM(t *testing.T) {
	_, err := ExtractClientCertJWK("", false)
	if err == nil {
		t.Fatal("expected error for empty PEM")
	}
}
