package main

import "testing"

// Annex A.1/A.2/A.3 of the EEBUS SHIP Pairing Service TS Specification v1.0.0.
//
// The converted markdown of the spec drops hyphens where the PDF wrapped
// lines, so the printed message M in section 7.4/A.3 reads "C8277H008F3" and
// "43652bk-2gt1" - those are wrong. The values below reconstruct M from the
// A.1/A.2 field values, with the hyphens, and this test pins the resulting
// digest against the value published in A.3 - independently of any other
// implementation. Do not change this test to assert against openeebus output.
const (
	annexADevAShipID      = "i:983327_u:C8277H008F-3"
	annexADevAFingerprint = "C74B7855D3479415F62CC01E5F6D9A93EBC676057D85417ADA16FD1384338943"
	annexADevASecret      = "7A37DCF81BDB50F8E92CFA4160CCB3DE"
	annexADevZShipID      = "i:46925_u:43652bk-2-gt1"
	annexADevZFingerprint = "2CC72E781F7A7D2A08D50196C50FEDF0F7BA583F43F76C8C0DDEC9EEF0D005B4"
	annexATrustNonce      = "BDCEE427FA7208DF3C1F2A749BA6F4D4"
	annexAExpectedDigest  = "BCBB62B2176DA2CEE545784CEB1F2A55E049451B12A549C98E8CA213F001DA25"
)

func TestCalcDigest_AnnexAVector(t *testing.T) {
	digest, err := calcDigest(
		annexADevASecret,
		annexATrustNonce,
		annexADevAShipID,
		annexADevAFingerprint,
		annexADevZShipID,
		annexADevZFingerprint,
	)
	if err != nil {
		t.Fatalf("calcDigest returned error: %v", err)
	}

	if digest != annexAExpectedDigest {
		t.Fatalf("digest mismatch\n got:  %s\n want: %s", digest, annexAExpectedDigest)
	}
}

func TestCalcDigest_WrongSecretProducesDifferentDigest(t *testing.T) {
	digest, err := calcDigest(
		"00000000000000000000000000000000",
		annexATrustNonce,
		annexADevAShipID,
		annexADevAFingerprint,
		annexADevZShipID,
		annexADevZFingerprint,
	)
	if err != nil {
		t.Fatalf("calcDigest returned error: %v", err)
	}

	if digest == annexAExpectedDigest {
		t.Fatal("wrong secret must not reproduce the Annex A digest")
	}
}
