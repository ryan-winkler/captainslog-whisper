// Package tls provides auto-generated self-signed TLS certificates.
// This enables HTTPS on local network domains (e.g., .home.arpa)
// which is required for browser microphone access on non-localhost origins.
package tls

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"log/slog"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"time"
)

// GenerateOrLoad creates or loads a self-signed TLS certificate.
// Certs are stored in certDir for persistence across restarts.
// The certificate covers localhost, the provided hostnames, and all
// local network IPs.
func GenerateOrLoad(certDir string, hostnames []string, logger *slog.Logger) (*tls.Config, error) {
	certFile := filepath.Join(certDir, "captainslog.crt")
	keyFile := filepath.Join(certDir, "captainslog.key")

	// Check if cert already exists and is valid
	if _, err := os.Stat(certFile); err == nil {
		if _, err := os.Stat(keyFile); err == nil {
			cert, err := tls.LoadX509KeyPair(certFile, keyFile)
			if err == nil {
				// Check expiry
				leaf, err := x509.ParseCertificate(cert.Certificate[0])
				if err == nil && time.Now().Before(leaf.NotAfter.Add(-24*time.Hour)) {
					logger.Info("loaded existing TLS certificate", "expires", leaf.NotAfter)
					return &tls.Config{Certificates: []tls.Certificate{cert}}, nil
				}
			}
			logger.Info("existing certificate expired or invalid, regenerating")
		}
	}

	// Generate new self-signed certificate
	if err := os.MkdirAll(certDir, 0700); err != nil {
		return nil, fmt.Errorf("create cert dir: %w", err)
	}

	privateKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, fmt.Errorf("generate key: %w", err)
	}

	serialNumber, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		return nil, fmt.Errorf("generate serial: %w", err)
	}

	template := &x509.Certificate{
		SerialNumber: serialNumber,
		Subject: pkix.Name{
			Organization: []string{"Captain's Log (self-signed)"},
			CommonName:   "Captain's Log Local",
		},
		NotBefore:             time.Now(),
		NotAfter:              time.Now().Add(365 * 24 * time.Hour), // 1 year
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
	}

	// Add SANs
	template.DNSNames = append(template.DNSNames, "localhost")
	template.DNSNames = append(template.DNSNames, hostnames...)
	template.IPAddresses = append(template.IPAddresses, net.ParseIP("127.0.0.1"), net.ParseIP("::1"))

	// Add all local IPs
	addrs, _ := net.InterfaceAddrs()
	for _, addr := range addrs {
		if ipNet, ok := addr.(*net.IPNet); ok && !ipNet.IP.IsLoopback() {
			template.IPAddresses = append(template.IPAddresses, ipNet.IP)
		}
	}

	certDER, err := x509.CreateCertificate(rand.Reader, template, template, &privateKey.PublicKey, privateKey)
	if err != nil {
		return nil, fmt.Errorf("create certificate: %w", err)
	}

	// Write cert
	certOut, err := os.Create(certFile)
	if err != nil {
		return nil, fmt.Errorf("write cert: %w", err)
	}
	pem.Encode(certOut, &pem.Block{Type: "CERTIFICATE", Bytes: certDER})
	certOut.Close()

	// Write key
	keyOut, err := os.OpenFile(keyFile, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0600)
	if err != nil {
		return nil, fmt.Errorf("write key: %w", err)
	}
	keyBytes, _ := x509.MarshalECPrivateKey(privateKey)
	pem.Encode(keyOut, &pem.Block{Type: "EC PRIVATE KEY", Bytes: keyBytes})
	keyOut.Close()

	logger.Info("generated new self-signed TLS certificate",
		"cert", certFile,
		"hostnames", template.DNSNames,
		"expires", template.NotAfter,
	)

	cert, err := tls.LoadX509KeyPair(certFile, keyFile)
	if err != nil {
		return nil, fmt.Errorf("load generated cert: %w", err)
	}

	return &tls.Config{Certificates: []tls.Certificate{cert}}, nil
}
