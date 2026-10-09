package v1

import (
	"fmt"
	"strings"
	"sync"
	"testing"
)

func TestConcurrentStrictDecodingKeepsTypesAndValuesIndependent(t *testing.T) {
	type numeric struct {
		Value int `json:"value"`
	}
	type textual struct {
		Value *string `json:"value"`
	}
	var workers sync.WaitGroup
	for i := 0; i < 16; i++ {
		workers.Add(1)
		go func() {
			defer workers.Done()
			for j := 0; j < 20; j++ {
				var n numeric
				if err := DecodeStrict(strings.NewReader(fmt.Sprintf(`{"value":%d}`, i*20+j)), &n); err != nil || n.Value != i*20+j {
					t.Error("numeric result crossed calls", err, n.Value)
				}
				var s textual
				if err := DecodeStrict(strings.NewReader(`{"value":null}`), &s); err != nil || s.Value != nil {
					t.Error("nullable type changed", err)
				}
				for _, raw := range []string{`{"value":null}`, `{"Value":1}`, `{"value":1,"value":2}`, `{"value":"text"}`, `{"value":1,"extra":true}`} {
					if DecodeStrict(strings.NewReader(raw), &n) == nil {
						t.Error("invalid record accepted after valid record")
					}
				}
			}
		}()
	}
	workers.Wait()
}
