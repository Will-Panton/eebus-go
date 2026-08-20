package main

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/enbility/zeroconf/v2"
)

// ShipPairing implements the devZ ("Control Unit") role described in the
// EEBUS SHIP Pairing Service TS Specification v1.0.0: given an
// administrator-supplied devA SHIP ID, certificate fingerprint and secret,
// it announces a single addCu-request over mDNS so a devA implementation
// (e.g. an Ohme heat pump) can discover and auto-trust this simulator.
//
// The simulator's own certificate is always P-256 (see cert.CreateCertificate),
// so trustCurve is always "secp256r1" (SHIP Pairing Service TS 6.2).
const (
	shipPairingServiceType = "_shippairing._tcp"
	shipPairingDomain      = "local."
	// SHIP Pairing Service TS 5.3: devZ must set an unused port and must
	// neither listen on it nor send data over it.
	shipPairingPort       = 1
	shipPairingTTLSeconds = 120 // SHIP Pairing Service TS 5.3

	shipPairingTxtVers    = "1"
	shipPairingParType    = "fpSha256"
	shipPairingAlg        = "hmacSha256"
	shipPairingType       = "addCu"
	shipPairingTrustCurve = "secp256r1"

	// SHIP Pairing Service TS 4.2: once paired, devZ announces for at most
	// 15 minutes of *uninterrupted* SHIP connection with devA.
	shipPairingAutoStopAfter = 15 * time.Minute
)

type PairingStatusPayload struct {
	Announcing      bool
	OwnShipID       string
	OwnFingerprint  string
	DevAShipID      string
	DevAFingerprint string
	DevASecret      string `json:",omitempty"`
	TrustNonce      string
	Digest          string
	Instance        string
	Error           string
}

type ShipPairing struct {
	mutex sync.Mutex

	ownShipID      string
	ownFingerprint string
	instanceName   string

	devAShipID      string
	devAFingerprint string
	trustNonce      string
	digest          string
	lastError       string

	server *zeroconf.Server

	pairedSKI string
	stopTimer *time.Timer
}

// NewShipPairing creates a devZ pairing announcer for this simulator's own
// SHIP ID, certificate and mDNS instance name.
func NewShipPairing(ownShipID string, ownCertDER []byte, instanceName string) *ShipPairing {
	return &ShipPairing{
		ownShipID:      ownShipID,
		ownFingerprint: CertificateFingerprint(ownCertDER),
		instanceName:   instanceName,
	}
}

// CertificateFingerprint returns the SHA-256 fingerprint of a DER encoded
// certificate, hex encoded with uppercase digits (SHIP Pairing Service TS 6.2).
func CertificateFingerprint(certDER []byte) string {
	sum := sha256.Sum256(certDER)
	return strings.ToUpper(hex.EncodeToString(sum[:]))
}

func randomHex128() (string, error) {
	buf := make([]byte, 16) // 128 bit, SHIP Pairing Service TS 6.2/6.3
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return strings.ToUpper(hex.EncodeToString(buf)), nil
}

// buildDigestMessage constructs the canonical message M exactly as required
// by SHIP Pairing Service TS 7.4. Field order and content must not deviate.
func buildDigestMessage(forID, forPar, trustID, trustPar, trustNonce string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "txtvers=%s;", shipPairingTxtVers)
	fmt.Fprintf(&b, "parType=%s;", shipPairingParType)
	fmt.Fprintf(&b, "forId=%s;", forID)
	fmt.Fprintf(&b, "forPar=%s;", forPar)
	fmt.Fprintf(&b, "trustId=%s;", trustID)
	fmt.Fprintf(&b, "trustPar=%s;", trustPar)
	fmt.Fprintf(&b, "trustCurve=%s;", shipPairingTrustCurve)
	fmt.Fprintf(&b, "type=%s;", shipPairingType)
	fmt.Fprintf(&b, "trustNonce=%s;", trustNonce)
	fmt.Fprintf(&b, "alg=%s;", shipPairingAlg)
	return b.String()
}

// calcDigest implements SHIP Pairing Service TS chapter 7:
// K = devASecret || trustNonce, M per buildDigestMessage(),
// digest = HMAC-SHA256_K(M).
func calcDigest(devASecretHex, trustNonceHex, forID, forPar, trustID, trustPar string) (string, error) {
	secret, err := hex.DecodeString(devASecretHex)
	if err != nil || len(secret) != 16 {
		return "", errors.New("devA secret must be exactly 32 hex digits (128 bit)")
	}

	nonce, err := hex.DecodeString(trustNonceHex)
	if err != nil || len(nonce) != 16 {
		return "", errors.New("trustNonce must be exactly 32 hex digits (128 bit)")
	}

	key := make([]byte, 0, len(secret)+len(nonce))
	key = append(key, secret...)
	key = append(key, nonce...)

	message := buildDigestMessage(forID, forPar, trustID, trustPar, trustNonceHex)

	mac := hmac.New(sha256.New, key)
	mac.Write([]byte(message))
	return strings.ToUpper(hex.EncodeToString(mac.Sum(nil))), nil
}

// Status returns a snapshot of the current pairing announcement, safe to
// send to the frontend. It never echoes the devA secret back.
func (p *ShipPairing) Status() PairingStatusPayload {
	p.mutex.Lock()
	defer p.mutex.Unlock()

	return PairingStatusPayload{
		Announcing:      p.server != nil,
		OwnShipID:       p.ownShipID,
		OwnFingerprint:  p.ownFingerprint,
		DevAShipID:      p.devAShipID,
		DevAFingerprint: p.devAFingerprint,
		TrustNonce:      p.trustNonce,
		Digest:          p.digest,
		Instance:        p.instanceName,
		Error:           p.lastError,
	}
}

// Start announces a new addCu-request for the given devA, per SHIP Pairing
// Service TS chapter 8. Stop() any previous announcement first.
func (p *ShipPairing) Start(devAShipID, devAFingerprint, devASecretHex string) PairingStatusPayload {
	p.mutex.Lock()
	defer p.mutex.Unlock()

	devAShipID = strings.TrimSpace(devAShipID)
	devAFingerprint = strings.ToUpper(strings.TrimSpace(devAFingerprint))
	devASecretHex = strings.ToUpper(strings.TrimSpace(devASecretHex))

	if p.server != nil {
		p.lastError = "pairing announcement is already running; stop it first"
		return p.statusLocked()
	}

	if devAShipID == "" || devAFingerprint == "" || devASecretHex == "" {
		p.lastError = "devA SHIP ID, fingerprint and secret are all required"
		return p.statusLocked()
	}

	if decoded, err := hex.DecodeString(devAFingerprint); err != nil || len(decoded) != 32 {
		p.lastError = "devA fingerprint must be exactly 64 hex digits (SHA-256)"
		return p.statusLocked()
	}

	nonce, err := randomHex128()
	if err != nil {
		p.lastError = "failed to generate trustNonce: " + err.Error()
		return p.statusLocked()
	}

	digest, err := calcDigest(devASecretHex, nonce, devAShipID, devAFingerprint, p.ownShipID, p.ownFingerprint)
	if err != nil {
		p.lastError = err.Error()
		return p.statusLocked()
	}

	txt := []string{
		"txtvers=" + shipPairingTxtVers,
		"parType=" + shipPairingParType,
		"forId=" + devAShipID,
		"forPar=" + devAFingerprint,
		"trustId=" + p.ownShipID,
		"trustPar=" + p.ownFingerprint,
		"trustCurve=" + shipPairingTrustCurve,
		"type=" + shipPairingType,
		"trustNonce=" + nonce,
		"alg=" + shipPairingAlg,
		"digest=" + digest,
	}

	server, err := zeroconf.Register(
		p.instanceName,
		shipPairingServiceType,
		shipPairingDomain,
		shipPairingPort,
		txt,
		nil,
		zeroconf.TTL(shipPairingTTLSeconds),
	)
	if err != nil {
		p.lastError = "failed to announce shippairing service: " + err.Error()
		return p.statusLocked()
	}

	p.server = server
	p.devAShipID = devAShipID
	p.devAFingerprint = devAFingerprint
	p.trustNonce = nonce
	p.digest = digest
	p.lastError = ""
	p.pairedSKI = ""
	p.cancelTimerLocked()

	return p.statusLocked()
}

// Stop deletes the current shippairing announcement (mDNS "goodbye"), per
// SHIP Pairing Service TS 4.2/5.5.
func (p *ShipPairing) Stop() PairingStatusPayload {
	p.mutex.Lock()
	defer p.mutex.Unlock()

	p.cancelTimerLocked()

	if p.server != nil {
		p.server.Shutdown()
		p.server = nil
	}

	p.devAShipID = ""
	p.devAFingerprint = ""
	p.trustNonce = ""
	p.digest = ""
	p.pairedSKI = ""
	p.lastError = ""

	return p.statusLocked()
}

func (p *ShipPairing) statusLocked() PairingStatusPayload {
	return PairingStatusPayload{
		Announcing:      p.server != nil,
		OwnShipID:       p.ownShipID,
		OwnFingerprint:  p.ownFingerprint,
		DevAShipID:      p.devAShipID,
		DevAFingerprint: p.devAFingerprint,
		TrustNonce:      p.trustNonce,
		Digest:          p.digest,
		Instance:        p.instanceName,
		Error:           p.lastError,
	}
}

func (p *ShipPairing) cancelTimerLocked() {
	if p.stopTimer != nil {
		p.stopTimer.Stop()
		p.stopTimer = nil
	}
}

// NoteShipID records which SKI belongs to which SHIP ID, as reported by the
// underlying SHIP stack, so a later connect/disconnect from that SKI can be
// matched back to the devA this pairing announcement targets.
func (p *ShipPairing) NoteShipID(ski, shipID string) {
	p.mutex.Lock()
	defer p.mutex.Unlock()

	if p.devAShipID != "" && shipID == p.devAShipID {
		p.pairedSKI = ski
	}
}

// NoteConnected starts (or restarts) the 15-minute uninterrupted-connection
// timer for the paired devA (SHIP Pairing Service TS 4.2, step 3).
func (p *ShipPairing) NoteConnected(ski string) {
	p.mutex.Lock()
	defer p.mutex.Unlock()

	if p.server == nil || p.pairedSKI == "" || ski != p.pairedSKI {
		return
	}

	p.cancelTimerLocked()
	p.stopTimer = time.AfterFunc(shipPairingAutoStopAfter, func() {
		p.Stop()
	})
}

// NoteDisconnected cancels the uninterrupted-connection timer: per the spec,
// an interruption restarts the 15 minutes from zero on the next connection.
func (p *ShipPairing) NoteDisconnected(ski string) {
	p.mutex.Lock()
	defer p.mutex.Unlock()

	if p.pairedSKI == "" || ski != p.pairedSKI {
		return
	}

	p.cancelTimerLocked()
}
