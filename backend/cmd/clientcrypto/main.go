//go:build js && wasm

package main

import (
	"encoding/base64"
	"errors"
	"fmt"
	"syscall/js"

	"github.com/farrelta/e-voting-ksskj/backend-go/cryptoutil"
)

// An election can never have more candidates than HE slots, this keeps the check cheap and well below the 8192-slot limit of the fixed parameter set.
const maxCandidates = 1024

func fail(err error) any {
	return map[string]any{"ok": false, "error": err.Error()}
}

func encryptBallot(_ js.Value, args []js.Value) (result any) {
	defer func() {
		if r := recover(); r != nil {
			result = fail(fmt.Errorf("encryption failed: %v", r))
		}
	}()

	if len(args) != 4 ||
		args[0].Type() != js.TypeString || args[1].Type() != js.TypeString ||
		args[2].Type() != js.TypeNumber || args[3].Type() != js.TypeNumber {
		return fail(errors.New("expected (hePublicKey, kemPublicKey, candidateIndex, candidateCount)"))
	}

	hePub, err := base64.StdEncoding.DecodeString(args[0].String())
	if err != nil || len(hePub) == 0 {
		return fail(errors.New("invalid HE public key"))
	}
	kemPub, err := base64.StdEncoding.DecodeString(args[1].String())
	if err != nil || len(kemPub) == 0 {
		return fail(errors.New("invalid KEM public key"))
	}

	index, count := args[2].Int(), args[3].Int()
	if count < 1 || count > maxCandidates {
		return fail(fmt.Errorf("candidate count must be between 1 and %d", maxCandidates))
	}
	if index < 0 || index >= count {
		return fail(fmt.Errorf("candidate %d is outside 0..%d", index, count-1))
	}

	ciphertext, err := cryptoutil.EncryptVote(hePub, index, count)
	if err != nil {
		return fail(err)
	}
	envelope, err := cryptoutil.SealEnvelope(kemPub, ciphertext)
	if err != nil {
		return fail(err)
	}

	return map[string]any{
		"ok":            true,
		"encryptedVote": base64.StdEncoding.EncodeToString(envelope),
	}
}

func main() {
	api := js.Global().Get("Object").New()
	api.Set("encryptBallot", js.FuncOf(encryptBallot))
	js.Global().Set("evotingCrypto", api)

	// Keep the Go runtime alive so the browser can keep calling into it.
	select {}
}
