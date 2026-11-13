//go:build js && wasm

package main

import (
	"encoding/base64"
	"syscall/js"

	"github.com/farrelta/e-voting-ksskj/backend-go/anonymousballot"
)

func prepare(_ js.Value, args []js.Value) any {
	if len(args) != 4 {
		return map[string]any{"error": "invalid ballot parameters"}
	}
	scope, scopeErr := base64.StdEncoding.DecodeString(args[0].String())
	key, keyErr := base64.StdEncoding.DecodeString(args[1].String())
	if scopeErr != nil || len(scope) != 80 || keyErr != nil {
		return map[string]any{"error": "invalid ballot parameters"}
	}
	vote, proof, err := anonymousballot.Prepare(anonymousballot.Scope(scope), key, args[2].Int(), args[3].Int())
	if err != nil {
		return map[string]any{"error": err.Error()}
	}
	return map[string]any{
		"encrypted_vote": base64.StdEncoding.EncodeToString(vote),
		"proof":          base64.StdEncoding.EncodeToString(proof),
	}
}

func main() {
	js.Global().Set("evotingPrepareAnonymousBallot", js.FuncOf(prepare))
	select {}
}
