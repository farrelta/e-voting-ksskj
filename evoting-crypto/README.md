# evoting-crypto

Pure Go library (no server, no database, no HTTP) implementing the three
cryptographic layers for the e-voting project:

- **ML-KEM-768** (`kem.go`) — post-quantum key exchange for securing the
  session channel after the campus gateway login.
- **ML-DSA-65** (`signature.go`) — post-quantum digital signature over each
  ballot's ciphertext, proving authenticity/integrity without revealing the
  vote.
- **BFV homomorphic encryption** (`he.go`, via Lattigo v6's scale-invariant
  `bgv` mode — see the note in that file) — lets the backend sum encrypted
  votes directly, so no individual ballot is ever decrypted.

Since your teammate's backend is also Go, this package is meant to be called
**directly as a function library** — no HTTP calls, no separate service to
deploy. `example_usage.go` (ignored by the Go build, kept purely as a
reference) shows exactly how each function gets called from a handler.

## Requirements

- **Go 1.24 or newer** (required by `tuneinsight/lattigo/v6`).

## Two ways to merge this into your teammate's repo

### Option 1 — Copy the package in directly (simplest)

Copy the `cryptoutil/` folder into your teammate's existing Go module, e.g.:

```
their-backend/
  go.mod                  <- their existing module, e.g. "module evoting-backend"
  main.go
  routes/
  cryptoutil/             <- paste this folder here
    kem.go
    signature.go
    he.go
```

Then merge the two `go.mod` files: keep their module name, but add these two
lines to their `require (...)` block:

```
require (
    github.com/cloudflare/circl v1.5.0
    github.com/tuneinsight/lattigo/v6 v6.2.0
)
```

Run `go mod tidy` in their repo root to fetch both. Then import it with
their module's path, e.g. if their module is `module evoting-backend`:

```go
import "evoting-backend/cryptoutil"
```

No `go.mod`/`go.sum` from this folder is needed once merged this way —
delete the one in this zip, it was only here so I could test it in isolation.

### Option 2 — Keep it as its own Go module (cleaner separation)

Push this folder as its own small repo (e.g. `github.com/yourusername/evoting-crypto`),
then from your teammate's backend run:

```bash
go get github.com/yourusername/evoting-crypto
```

and import it as:

```go
import "github.com/yourusername/evoting-crypto/cryptoutil"
```

This keeps your work cleanly separated (you own and version the crypto
package; your teammate's `go.mod` just lists it as a dependency like any
other library), which also reads well in a thesis as "a self-contained,
independently-tested cryptographic module."

**Recommendation:** Option 2 if you want clean ownership boundaries and the
ability to say "I built and unit-tested this as a standalone module" in your
defense. Option 1 if you'd rather keep everything in one repo for simplicity.

## Public API

```go
// Key generation (call once per election, when admin creates it)
func GenerateKEMKeyPair() (pubKey, privKey []byte, err error)
func GenerateDSAKeyPair() (pubKey, privKey []byte, err error)
func GenerateHEKeyPair() (pubKey, secKey []byte, err error)

// Vote submission (call once per voter, after auth/whitelist/already-voted checks)
func EncryptVote(hePubKey []byte, candidateID, numCandidates int) (ciphertext []byte, err error)
func SignCiphertext(dsaPrivKey, ciphertext []byte) (signature []byte, err error)

// Ballot verification (optional, call on ingest before storing a ballot)
func VerifySignature(dsaPubKey, ciphertext, signature []byte) (valid bool, err error)

// Tallying (call once, after voting closes, with every stored ciphertext)
func AggregateAndDecrypt(heSecretKey []byte, ciphertexts [][]byte, numCandidates int) (counts []uint64, err error)

// KEM session establishment (if you also want to wrap the post-gateway
// session channel itself, not just the ballot)
func Encapsulate(kemPubKey []byte) (ciphertext, sharedSecret []byte, err error)
func Decapsulate(kemPrivKey, ciphertext []byte) (sharedSecret []byte, err error)
```

Full call examples for each of these are in `example_usage.go`.

## Where secrets live now that there's no separate service

Previously (HTTP microservice design), `dsa_private_key` and `he_secret_key`
lived only inside the crypto service's request/response cycle. Now that this
is a library called in-process by the main backend, your teammate's backend
is directly responsible for:

- Storing `kem_private_key`, `dsa_private_key`, and `he_secret_key` encrypted
  at rest (or in a secrets manager / environment variable loaded at startup) —
  never in a plain database column.
- Loading `he_secret_key` into memory **only** at tally time, ideally, rather
  than keeping it resident for the whole life of the process.
- Making sure `dsa_private_key` is accessible to whatever handler processes
  vote submissions, but `he_secret_key` is NOT needed there at all (only at
  tally) — don't pass it around more broadly than it needs to be.

## Testing

```bash
go test ./cryptoutil/... -v
```

This runs `e2e_test.go`: 5 dummy votes across 3 candidates, through the full
encrypt → sign → verify → homomorphic tally pipeline, asserting the final
counts match and that a tampered ciphertext is correctly rejected.
