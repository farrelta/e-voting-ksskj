// Package anonymousballot implements a prototype single-choice ElGamal ballot.
package anonymousballot

import (
	"crypto/elliptic"
	"crypto/rand"
	"errors"
	"math/big"

	"github.com/cloudflare/circl/group"
)

const (
	pointSize     = 33
	scalarSize    = 32
	MaxCandidates = 32
	MaxBallots    = 10000
)

var curve = group.P256

type ciphertext struct{ a, b group.Element }

// Scope contains the election UUID, candidate manifest hash, and token digest.
type Scope [80]byte

func GenerateKey() ([]byte, []byte, error) {
	secret := curve.RandomNonZeroScalar(rand.Reader)
	public, err := curve.NewElement().MulGen(secret).MarshalBinaryCompress()
	if err != nil {
		return nil, nil, err
	}
	private, err := secret.MarshalBinary()
	return public, private, err
}

func point(raw []byte) (group.Element, error) {
	p := curve.NewElement()
	if len(raw) != pointSize || (raw[0] != 2 && raw[0] != 3) || p.UnmarshalBinary(raw) != nil || p.IsIdentity() {
		return nil, errors.New("invalid ballot point")
	}
	return p, nil
}

func scalar(raw []byte) (group.Scalar, error) {
	if len(raw) != scalarSize || new(big.Int).SetBytes(raw).Cmp(elliptic.P256().Params().N) >= 0 {
		return nil, errors.New("invalid ballot scalar")
	}
	s := curve.NewScalar()
	return s, s.UnmarshalBinary(raw)
}

func pointBytes(p group.Element) []byte {
	b, err := p.MarshalBinaryCompress()
	if err != nil {
		panic(err)
	}
	return b
}

func appendScalar(dst []byte, s group.Scalar) []byte {
	b, err := s.MarshalBinary()
	if err != nil {
		panic(err)
	}
	return append(dst, b...)
}

func add(a, b ciphertext) ciphertext {
	return ciphertext{curve.NewElement().Add(a.a, b.a), curve.NewElement().Add(a.b, b.b)}
}

func zero() ciphertext { return ciphertext{curve.Identity(), curve.Identity()} }

func statement(ct ciphertext, value int) ciphertext {
	if value == 0 {
		return ct
	}
	return ciphertext{ct.a, subtract(ct.b, curve.Generator())}
}

func subtract(x, y group.Element) group.Element {
	return curve.NewElement().Add(x, curve.NewElement().Neg(y))
}

// Reconstruct the two Chaum–Pedersen commitments from the response.
func commitments(key group.Element, st ciphertext, c, z group.Scalar) ciphertext {
	return ciphertext{
		subtract(curve.NewElement().MulGen(z), curve.NewElement().Mul(st.a, c)),
		subtract(curve.NewElement().Mul(key, z), curve.NewElement().Mul(st.b, c)),
	}
}

func commitment(key group.Element, w group.Scalar) ciphertext {
	return ciphertext{curve.NewElement().MulGen(w), curve.NewElement().Mul(key, w)}
}

func challenge(base []byte, index, kind byte, pairs ...ciphertext) group.Scalar {
	msg := make([]byte, 0, len(base)+2+2*pointSize*len(pairs))
	msg = append(append(msg, base...), index, kind)
	for _, pair := range pairs {
		for _, p := range []group.Element{pair.a, pair.b} {
			var encoded [pointSize]byte
			copy(encoded[:], pointBytes(p))
			msg = append(msg, encoded[:]...)
		}
	}
	return curve.HashToScalar(msg, []byte("evoting-p256-ballot-proof-v1"))
}

func transcript(scope Scope, pub, encrypted []byte, count int) []byte {
	out := append([]byte{byte(count)}, scope[:]...)
	out = append(out, pub...)
	return append(out, encrypted...)
}

func Prepare(scope Scope, pub []byte, count, selected int) ([]byte, []byte, error) {
	if count < 1 || count > MaxCandidates || selected < 0 || selected >= count {
		return nil, nil, errors.New("invalid ballot choice")
	}
	key, err := point(pub)
	if err != nil {
		return nil, nil, err
	}
	ciphertexts := make([]ciphertext, count)
	nonces := make([]group.Scalar, count)
	totalNonce := curve.NewScalar()
	encrypted := make([]byte, 0, count*2*pointSize)
	for i := range ciphertexts {
		for {
			r := curve.RandomNonZeroScalar(rand.Reader)
			ct := commitment(key, r)
			if i == selected {
				ct.b = curve.NewElement().Add(ct.b, curve.Generator())
			}
			if ct.b.IsIdentity() {
				continue
			}
			ciphertexts[i], nonces[i] = ct, r
			totalNonce = curve.NewScalar().Add(totalNonce, r)
			encrypted = append(append(encrypted, pointBytes(ct.a)...), pointBytes(ct.b)...)
			break
		}
	}
	base := transcript(scope, pub, encrypted, count)
	proof := make([]byte, 0, (4*count+2)*scalarSize)
	for i, ct := range ciphertexts {
		real := 0
		if i == selected {
			real = 1
		}
		fake := 1 - real
		var c, z [2]group.Scalar
		var t [2]ciphertext
		c[fake], z[fake] = curve.RandomScalar(rand.Reader), curve.RandomScalar(rand.Reader)
		t[fake] = commitments(key, statement(ct, fake), c[fake], z[fake])
		w := curve.RandomNonZeroScalar(rand.Reader)
		t[real] = commitment(key, w)
		c[real] = curve.NewScalar().Sub(challenge(base, byte(i), 0, t[:]...), c[fake])
		z[real] = curve.NewScalar().Add(w, curve.NewScalar().Mul(c[real], nonces[i]))
		for j := range c {
			proof = appendScalar(appendScalar(proof, c[j]), z[j])
		}
	}
	w := curve.RandomNonZeroScalar(rand.Reader)
	c := challenge(base, 0, 1, commitment(key, w))
	z := curve.NewScalar().Add(w, curve.NewScalar().Mul(c, totalNonce))
	return encrypted, appendScalar(appendScalar(proof, c), z), nil
}

func parseCiphertexts(encrypted []byte, count int) ([]ciphertext, error) {
	if count < 1 || count > MaxCandidates || len(encrypted) != count*2*pointSize {
		return nil, errors.New("invalid ballot ciphertext length")
	}
	result := make([]ciphertext, count)
	for i := range result {
		offset := i * 2 * pointSize
		a, err := point(encrypted[offset : offset+pointSize])
		if err != nil {
			return nil, err
		}
		b, err := point(encrypted[offset+pointSize : offset+2*pointSize])
		if err != nil {
			return nil, err
		}
		result[i] = ciphertext{a, b}
	}
	return result, nil
}

func Verify(scope Scope, pub []byte, count int, encrypted, proof []byte) error {
	key, err := point(pub)
	if err != nil {
		return err
	}
	ciphertexts, err := parseCiphertexts(encrypted, count)
	if err != nil {
		return err
	}
	if len(proof) != (4*count+2)*scalarSize {
		return errors.New("invalid ballot proof length")
	}
	base := transcript(scope, pub, encrypted, count)
	total := zero()
	read := func() (group.Scalar, error) {
		s, err := scalar(proof[:scalarSize])
		proof = proof[scalarSize:]
		return s, err
	}
	for i, ct := range ciphertexts {
		var t [2]ciphertext
		sum := curve.NewScalar()
		for j := range t {
			c, err := read()
			if err != nil {
				return err
			}
			z, err := read()
			if err != nil {
				return err
			}
			t[j] = commitments(key, statement(ct, j), c, z)
			sum = curve.NewScalar().Add(sum, c)
		}
		if !sum.IsEqual(challenge(base, byte(i), 0, t[:]...)) {
			return errors.New("invalid selection proof")
		}
		total = add(total, ct)
	}
	c, err := read()
	if err != nil {
		return err
	}
	z, err := read()
	if err != nil {
		return err
	}
	if !c.IsEqual(challenge(base, 0, 1, commitments(key, statement(total, 1), c, z))) {
		return errors.New("invalid single-choice proof")
	}
	return nil
}

// Tally accepts previously verified ciphertexts and decrypts only their sum.
func Tally(secret []byte, count int, ballots [][]byte) ([]uint64, error) {
	if count < 1 || count > MaxCandidates || len(ballots) > MaxBallots {
		return nil, errors.New("ballot tally outside prototype bounds")
	}
	key, err := scalar(secret)
	if err != nil || key.IsZero() {
		return nil, errors.New("invalid tally key")
	}
	totals := make([]ciphertext, count)
	for i := range totals {
		totals[i] = zero()
	}
	for _, ballot := range ballots {
		cts, err := parseCiphertexts(ballot, count)
		if err != nil {
			return nil, err
		}
		for i, ct := range cts {
			totals[i] = add(totals[i], ct)
		}
	}
	lookup := make(map[string]uint64, len(ballots)+1)
	p := curve.Identity()
	for i := 0; i <= len(ballots); i++ {
		lookup[string(pointBytes(p))] = uint64(i)
		p = curve.NewElement().Add(p, curve.Generator())
	}
	results := make([]uint64, count)
	var sum uint64
	for i, ct := range totals {
		message := subtract(ct.b, curve.NewElement().Mul(ct.a, key))
		value, ok := lookup[string(pointBytes(message))]
		if !ok {
			return nil, errors.New("candidate tally outside ballot count")
		}
		results[i], sum = value, sum+value
	}
	if sum != uint64(len(ballots)) {
		return nil, errors.New("tally does not match ballot count")
	}
	return results, nil
}
