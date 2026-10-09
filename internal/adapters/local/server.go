package local

import (
	"context"
	"encoding/json"
	"github.com/third-party-api-key-hub/internal/application"
	v1 "github.com/third-party-api-key-hub/internal/contract/v1"
	"io"
	"net"
	"net/http"
	"time"
)

type actorKey struct{}

func Server(engine *application.Engine, authorize func(application.Actor) application.Actor) *http.Server {
	return &http.Server{ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 15 * time.Second, WriteTimeout: 15 * time.Second, IdleTimeout: 1 * time.Second, MaxHeaderBytes: 8192,
		ConnContext: func(ctx context.Context, c net.Conn) context.Context {
			actor, err := Peer(c)
			if err != nil {
				return ctx
			}
			return context.WithValue(ctx, actorKey{}, actor)
		},
		Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.Header().Set("Connection", "close")
			r.Close = true
			fail := func(err error) { json.NewEncoder(w).Encode(v1.SafeError(err)) }
			actor, identified := r.Context().Value(actorKey{}).(application.Actor)
			if !identified {
				fail(v1.Fail(v1.ErrPermissionDenied))
				// Linux reserves UID -1; never label an unidentified peer as root.
				engine.RecordBoundaryRejection(r.Context(), application.Actor{UID: ^uint32(0), Name: "unidentified"}, v1.ErrPermissionDenied)
				return
			}
			actor = authorize(actor)
			if !actor.Read && !actor.Manage {
				fail(v1.Fail(v1.ErrPermissionDenied))
				engine.RecordBoundaryRejection(r.Context(), actor, v1.ErrPermissionDenied)
				return
			}
			if r.Method != "POST" || r.URL.Path != "/v1/operations" || r.URL.RawQuery != "" || r.Header.Get("Content-Type") != "application/json" {
				fail(v1.Fail(v1.ErrInputInvalid))
				engine.RecordBoundaryRejection(r.Context(), actor, v1.ErrInputInvalid)
				return
			}
			var req v1.OperationRequest
			if err := v1.DecodeStrict(io.LimitReader(r.Body, v1.MaxRequestBytes+1), &req); err != nil {
				fail(v1.Fail(v1.ErrInputInvalid))
				engine.RecordBoundaryRejection(r.Context(), actor, v1.ErrInputInvalid)
				return
			}
			ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
			defer cancel()
			result, err := engine.Execute(ctx, req, actor)
			if err != nil {
				fail(err)
				return
			}
			json.NewEncoder(w).Encode(result)
		}),
	}
}
