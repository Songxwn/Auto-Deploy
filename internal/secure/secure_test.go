package secure

import "testing"

func TestRoundTrip(t *testing.T) {
	b := NewBox("test-secret")
	for _, plain := range []string{"p@ssw0rd!", "你好，世界", "-----BEGIN OPENSSH PRIVATE KEY-----\nabc\n-----END OPENSSH PRIVATE KEY-----"} {
		enc, err := b.Encrypt(plain)
		if err != nil {
			t.Fatalf("Encrypt: %v", err)
		}
		if enc == plain {
			t.Fatal("ciphertext equals plaintext")
		}
		got, err := b.Decrypt(enc)
		if err != nil {
			t.Fatalf("Decrypt: %v", err)
		}
		if got != plain {
			t.Fatalf("got %q, want %q", got, plain)
		}
	}
}

func TestEmpty(t *testing.T) {
	b := NewBox("k")
	enc, err := b.Encrypt("")
	if err != nil || enc != "" {
		t.Fatalf("empty encrypt: %q %v", enc, err)
	}
	dec, err := b.Decrypt("")
	if err != nil || dec != "" {
		t.Fatalf("empty decrypt: %q %v", dec, err)
	}
}

func TestWrongKey(t *testing.T) {
	enc, err := NewBox("key-a").Encrypt("secret")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := NewBox("key-b").Decrypt(enc); err == nil {
		t.Fatal("decrypt with wrong key should fail")
	}
}

func TestTamper(t *testing.T) {
	b := NewBox("k")
	enc, err := b.Encrypt("secret")
	if err != nil {
		t.Fatal(err)
	}
	tampered := []byte(enc)
	tampered[len(tampered)-2] ^= 0x01
	if _, err := b.Decrypt(string(tampered)); err == nil {
		t.Fatal("tampered ciphertext should fail")
	}
}
