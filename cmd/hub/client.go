package main

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	v1 "github.com/third-party-api-key-hub/internal/contract/v1"
	"io"
	"net"
	"net/http"
	"os"
	"time"
)

func main() {
	flags := flag.NewFlagSet("hub", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	socket := flags.String("socket", "/tmp/api-key-hub-fixture/hub.sock", "local Unix service socket")
	interactive := flags.Bool("interactive", false, "read import value with hidden terminal input")
	if flags.Parse(os.Args[1:]) != nil || flags.NArg() != 0 {
		json.NewEncoder(os.Stdout).Encode(v1.Fail(v1.ErrInputInvalid).Response)
		os.Exit(1)
	}
	os.Exit(runMode(os.Stdin, os.Stdout, os.Stderr, *socket, *interactive))
}
func run(in io.Reader, out io.Writer, socket string) int {
	return runMode(in, out, io.Discard, socket, false)
}
func runMode(in io.Reader, out io.Writer, diagnostics io.Writer, socket string, interactive bool) int {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	fail := func(code v1.ErrorCode) int {
		raw, _ := json.Marshal(v1.Fail(code).Response)
		out.Write(append(raw, '\n'))
		return 1
	}
	var req v1.OperationRequest
	decoded := make(chan error, 1)
	go func() { decoded <- v1.DecodeStrict(in, &req) }()
	select {
	case err := <-decoded:
		if err != nil || v1.ValidateOperationRequest(req) != nil {
			return fail(v1.ErrInputInvalid)
		}
	case <-ctx.Done():
		if closer, ok := in.(io.Closer); ok {
			closer.Close()
		}
		return fail(v1.ErrInputInvalid)
	}
	if v1.ValidateOperationRequest(req) != nil {
		return fail(v1.ErrInputInvalid)
	}
	if interactive {
		if req.Operation != "credential.import" {
			return fail(v1.ErrInputInvalid)
		}
		var fields map[string]json.RawMessage
		if json.Unmarshal(req.Payload, &fields) != nil || fields == nil {
			return fail(v1.ErrInputInvalid)
		}
		if _, exists := fields["value"]; exists {
			return fail(v1.ErrInputInvalid)
		}
		value, err := hiddenValueContext(ctx, diagnostics)
		if err != nil {
			return fail(v1.ErrInputInvalid)
		}
		fields["value"], _ = json.Marshal(value)
		req.Payload, _ = json.Marshal(fields)
	}
	if v1.ValidateOperationPayload(req) != nil {
		return fail(v1.ErrInputInvalid)
	}
	raw, err := json.Marshal(req)
	if err != nil {
		return fail(v1.ErrInputInvalid)
	}
	transport := &http.Transport{DisableKeepAlives: true, DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, "unix", socket)
	}}
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport, Timeout: 15 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	request, err := http.NewRequestWithContext(ctx, "POST", "http://localhost/v1/operations", bytes.NewReader(raw))
	if err != nil {
		return fail(v1.ErrInternal)
	}
	request.Header.Set("Content-Type", "application/json")
	request.Close = true
	response, err := client.Do(request)
	if err != nil {
		return fail(v1.ErrInternal)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return fail(v1.ErrInternal)
	}
	result, code, err := validateResponse(response.Body, req.Operation)
	if err != nil {
		return fail(v1.ErrInternal)
	}
	if code == 0 && req.Operation == "credential.query" {
		var selected v1.QueryInput
		var got v1.QueryResult
		if v1.DecodeStrict(bytes.NewReader(req.Payload), &selected) != nil || json.Unmarshal(result, &got) != nil || !matchesSelection(selected, got.Identity) {
			return fail(v1.ErrInternal)
		}
	}
	// Complete validation happens before the first output byte. Do not append a second JSON on an I/O failure.
	if _, err = out.Write(append(result, '\n')); err != nil {
		return 1
	}
	return code
}
func matchesSelection(q v1.QueryInput, i v1.Identity) bool {
	return (q.AppID == "" || q.AppID == i.AppID) && (q.ExternalShopID == "" || q.ExternalShopID == i.ExternalShopID) && (q.BrandID == "" || q.BrandID == i.BrandID) && (q.InternalShopID == "" || q.InternalShopID == i.InternalShopID)
}
