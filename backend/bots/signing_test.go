package bots

import "testing"

func TestSignVerifyRoundTrip(t *testing.T) {
	secret := GenerateSecret()
	ts := "1700000000"
	body := []byte(`{"type":"app_mention","event":{"text":"hi"}}`)

	sig := Sign(ts, body, secret)
	if len(sig) < 4 || sig[:3] != "v0=" {
		t.Fatalf("signature missing v0= prefix: %q", sig)
	}
	if !Verify(ts, body, secret, sig) {
		t.Fatalf("Verify rejected a signature it produced")
	}
}

func TestSignDeterministic(t *testing.T) {
	secret := "vbs_fixed"
	ts := "1700000000"
	body := []byte("hello")
	if Sign(ts, body, secret) != Sign(ts, body, secret) {
		t.Fatalf("Sign is not deterministic")
	}
}

func TestVerifyWrongSecret(t *testing.T) {
	ts := "1700000000"
	body := []byte("payload")
	sig := Sign(ts, body, "secret-A")

	if Verify(ts, body, "secret-B", sig) {
		t.Fatalf("Verify accepted a signature made with a different secret")
	}
}

func TestVerifyTamperedBody(t *testing.T) {
	secret := "s"
	ts := "1700000000"
	sig := Sign(ts, []byte("original"), secret)
	if Verify(ts, []byte("tampered"), secret, sig) {
		t.Fatalf("Verify accepted a tampered body")
	}
	if Verify("1700000001", []byte("original"), secret, sig) {
		t.Fatalf("Verify accepted a different timestamp")
	}
}

func TestHashTokenStableAndDistinct(t *testing.T) {
	a := GenerateToken()
	b := GenerateToken()
	if a == b {
		t.Fatalf("GenerateToken produced duplicates")
	}
	if HashToken(a) != HashToken(a) {
		t.Fatalf("HashToken not stable")
	}
	if HashToken(a) == HashToken(b) {
		t.Fatalf("distinct tokens hashed equal")
	}
	if HashToken(a) == a {
		t.Fatalf("HashToken returned the plaintext")
	}
}
