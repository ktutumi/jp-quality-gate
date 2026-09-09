//go:build js && wasm

// Command jp-quality-gate-wasm exposes the core quality gate to JavaScript.
package main

import (
	"bytes"
	"compress/gzip"
	"encoding/json"
	"errors"
	"io"
	"math"
	"syscall/js"
	"unicode/utf8"

	"github.com/ktutumi/jp-quality-gate/internal/cj"
	"github.com/ktutumi/jp-quality-gate/internal/embedded"
	"github.com/ktutumi/jp-quality-gate/internal/gate"
	"github.com/ktutumi/jp-quality-gate/internal/unihan"
)

const (
	defaultCJMinCJK = 4
	defaultCJMinGap = 0.15
	maxTextBytes    = 256 * 1024

	invalidRequestError = "invalid request"
	internalError       = "internal error"
	initializationError = "initialization failed"
)

var checkCallback js.Func

type bridgeRequest struct {
	Text    *string        `json:"text"`
	Options *bridgeOptions `json:"options"`
}

type bridgeOptions struct {
	CJMinCJK         int     `json:"cj_min_cjk"`
	CJMinGap         float64 `json:"cj_min_gap"`
	IncludeCode      bool    `json:"include_code"`
	WarningsAsErrors bool    `json:"warnings_as_errors"`
}

type bridgeError struct {
	Pass          bool   `json:"pass"`
	InternalError string `json:"internal_error"`
}

func main() {
	defer func() {
		if recover() != nil {
			notifyReady(initializationError)
		}
		select {}
	}()

	core, initialized := initializeCore()
	installCheck(core)
	if initialized {
		notifyReady("")
	} else {
		notifyReady(initializationError)
	}
}

func initializeCore() (core *gate.Gate, initialized bool) {
	defer func() {
		if recover() != nil {
			core = nil
			initialized = false
		}
	}()

	unihanScanner, err := loadEmbeddedUnihan()
	if err != nil {
		return nil, false
	}
	classifier, err := cj.Load()
	if err != nil {
		return nil, false
	}
	return &gate.Gate{Unihan: unihanScanner, CJ: classifier}, true
}

func loadEmbeddedUnihan() (*unihan.Scanner, error) {
	reader, err := gzip.NewReader(bytes.NewReader(embedded.UnihanTableGZIP))
	if err != nil {
		return nil, err
	}
	data, readErr := io.ReadAll(reader)
	closeErr := reader.Close()
	if readErr != nil {
		return nil, readErr
	}
	if closeErr != nil {
		return nil, closeErr
	}
	return unihan.LoadBytes(data, "embedded:unihan-suspicious-18.0.0.json.gz")
}

func installCheck(core *gate.Gate) {
	checkCallback = js.FuncOf(func(this js.Value, args []js.Value) (response interface{}) {
		response = errorJSON(internalError)
		defer func() {
			if recover() != nil {
				response = errorJSON(internalError)
			}
		}()

		if core == nil {
			return errorJSON(initializationError)
		}
		if len(args) != 1 || args[0].Type() != js.TypeString {
			return errorJSON(invalidRequestError)
		}
		return check(core, args[0].String())
	})
	js.Global().Set("__jpqgCheck", checkCallback)
}

func notifyReady(errString string) {
	defer func() { _ = recover() }()
	ready := js.Global().Get("__jpqgReady")
	if ready.Type() != js.TypeFunction {
		return
	}
	if errString == "" {
		ready.Invoke(js.Null())
		return
	}
	ready.Invoke(js.ValueOf(errString))
}

func check(g *gate.Gate, requestJSON string) string {
	request, ok := parseRequest(requestJSON)
	if !ok {
		return errorJSON(invalidRequestError)
	}

	result := g.Check(*request.Text, gate.Options{
		IncludeCode:      request.Options.IncludeCode,
		CJMinCJK:         request.Options.CJMinCJK,
		CJMinGap:         request.Options.CJMinGap,
		WarningsAsErrors: request.Options.WarningsAsErrors,
	})
	data, err := json.Marshal(result)
	if err != nil {
		return errorJSON(internalError)
	}
	return string(data)
}

func parseRequest(requestJSON string) (bridgeRequest, bool) {
	request := bridgeRequest{
		Options: &bridgeOptions{CJMinCJK: defaultCJMinCJK, CJMinGap: defaultCJMinGap},
	}
	if decodeExact([]byte(requestJSON), &request) != nil || request.Text == nil || request.Options == nil {
		return bridgeRequest{}, false
	}
	if !utf8.ValidString(*request.Text) || len(*request.Text) > maxTextBytes {
		return bridgeRequest{}, false
	}
	if request.Options.CJMinCJK < 1 || request.Options.CJMinGap < 0 || request.Options.CJMinGap > 1 || math.IsNaN(request.Options.CJMinGap) || math.IsInf(request.Options.CJMinGap, 0) {
		return bridgeRequest{}, false
	}
	return request, true
}

func (o *bridgeOptions) UnmarshalJSON(data []byte) error {
	if bytes.Equal(bytes.TrimSpace(data), []byte("null")) {
		return errors.New("options must be an object")
	}
	var raw map[string]json.RawMessage
	if err := decodeExact(data, &raw); err != nil {
		return err
	}
	for _, value := range raw {
		if bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
			return errors.New("option must not be null")
		}
	}
	type options bridgeOptions
	parsed := options(*o)
	if err := decodeExact(data, &parsed); err != nil {
		return err
	}
	*o = bridgeOptions(parsed)
	return nil
}

func decodeExact(data []byte, value any) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(value); err != nil {
		return err
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		if err == nil {
			return errors.New("trailing JSON value")
		}
		return err
	}
	return nil
}

func errorJSON(message string) string {
	data, err := json.Marshal(bridgeError{Pass: false, InternalError: message})
	if err != nil {
		return `{"pass":false,"internal_error":"internal error"}`
	}
	return string(data)
}
