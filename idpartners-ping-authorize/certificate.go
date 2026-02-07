package main

import (
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/elliptic"
	"crypto/rsa"
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
	"fmt"
	"math/big"
)

// ExtractClientCertJWK parses a PEM certificate chain and extracts the leaf public key as a JWK.
// If includeFullChain is true, all certificates are included in x5c; otherwise only the leaf.
func ExtractClientCertJWK(pemChain string, includeFullChain bool) (*JWK, error) {
	certs, err := parsePEMCertificates(pemChain)
	if err != nil {
		return nil, fmt.Errorf("failed to parse certificate chain: %w", err)
	}
	if len(certs) == 0 {
		return nil, fmt.Errorf("no certificates found in PEM data")
	}

	leaf := certs[0]
	jwk, err := publicKeyToJWK(leaf.PublicKey)
	if err != nil {
		return nil, fmt.Errorf("failed to convert public key to JWK: %w", err)
	}

	// Build x5c chain (standard Base64, not Base64URL, per RFC 7517 §4.7)
	if includeFullChain {
		jwk.X5C = make([]string, len(certs))
		for i, cert := range certs {
			jwk.X5C[i] = base64.StdEncoding.EncodeToString(cert.Raw)
		}
	} else {
		jwk.X5C = []string{base64.StdEncoding.EncodeToString(leaf.Raw)}
	}

	return jwk, nil
}

// parsePEMCertificates parses all certificates from a PEM-encoded chain.
func parsePEMCertificates(pemData string) ([]*x509.Certificate, error) {
	var certs []*x509.Certificate
	rest := []byte(pemData)

	for {
		var block *pem.Block
		block, rest = pem.Decode(rest)
		if block == nil {
			break
		}
		if block.Type != "CERTIFICATE" {
			continue
		}
		cert, err := x509.ParseCertificate(block.Bytes)
		if err != nil {
			return nil, err
		}
		certs = append(certs, cert)
	}

	return certs, nil
}

// publicKeyToJWK converts a crypto public key to a JWK struct.
// Supports RSA, ECDSA, and Ed25519 public keys.
func publicKeyToJWK(pub interface{}) (*JWK, error) {
	switch key := pub.(type) {
	case *rsa.PublicKey:
		return rsaPublicKeyToJWK(key), nil
	case *ecdsa.PublicKey:
		return ecPublicKeyToJWK(key)
	case ed25519.PublicKey:
		return ed25519PublicKeyToJWK(key), nil
	default:
		return nil, fmt.Errorf("unsupported key type: %T", pub)
	}
}

func rsaPublicKeyToJWK(key *rsa.PublicKey) *JWK {
	return &JWK{
		Kty: "RSA",
		N:   base64URLEncodeBigInt(key.N),
		E:   base64URLEncodeInt(key.E),
	}
}

func ecPublicKeyToJWK(key *ecdsa.PublicKey) (*JWK, error) {
	crv, err := curveName(key.Curve)
	if err != nil {
		return nil, err
	}

	byteLen := curveByteLen(key.Curve)
	return &JWK{
		Kty: "EC",
		Crv: crv,
		X:   base64URLEncodePadded(key.X, byteLen),
		Y:   base64URLEncodePadded(key.Y, byteLen),
	}, nil
}

func ed25519PublicKeyToJWK(key ed25519.PublicKey) *JWK {
	return &JWK{
		Kty: "OKP",
		Crv: "Ed25519",
		X:   base64.RawURLEncoding.EncodeToString(key),
	}
}

func curveName(curve elliptic.Curve) (string, error) {
	switch curve {
	case elliptic.P256():
		return "P-256", nil
	case elliptic.P384():
		return "P-384", nil
	case elliptic.P521():
		return "P-521", nil
	default:
		return "", fmt.Errorf("unsupported EC curve: %v", curve.Params().Name)
	}
}

func curveByteLen(curve elliptic.Curve) int {
	bits := curve.Params().BitSize
	return (bits + 7) / 8
}

// base64URLEncodeBigInt encodes a big.Int as Base64URL without padding.
func base64URLEncodeBigInt(n *big.Int) string {
	return base64.RawURLEncoding.EncodeToString(n.Bytes())
}

// base64URLEncodeInt encodes an int as Base64URL without padding.
func base64URLEncodeInt(n int) string {
	b := big.NewInt(int64(n))
	return base64.RawURLEncoding.EncodeToString(b.Bytes())
}

// base64URLEncodePadded encodes a big.Int with left-zero-padding to the given byte length.
func base64URLEncodePadded(n *big.Int, byteLen int) string {
	b := n.Bytes()
	if len(b) < byteLen {
		padded := make([]byte, byteLen)
		copy(padded[byteLen-len(b):], b)
		b = padded
	}
	return base64.RawURLEncoding.EncodeToString(b)
}
