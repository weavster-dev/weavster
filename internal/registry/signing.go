package registry

import "crypto/ed25519"

// Sign signs a module digest with an Ed25519 private key (gap #2).
func Sign(key ed25519.PrivateKey, digest string) []byte {
	return ed25519.Sign(key, []byte(digest))
}

// Verify checks a module signature over its digest. A nil public key disables
// verification (local DX).
func Verify(pub ed25519.PublicKey, digest string, sig []byte) bool {
	if pub == nil {
		return true
	}
	return ed25519.Verify(pub, []byte(digest), sig)
}
