// Insurance verification service — Phase-1 scaffold.
//
// Phase-2 work: implement provider connectors per country (e.g. CoB
// register equivalents, EU Green Card system, national insurance bureaus).
package main

import (
	"context"
	"net/http"

	"github.com/icofcucam/naditos/packages/go-common/config"
	"github.com/icofcucam/naditos/packages/go-common/httpx"
	"github.com/icofcucam/naditos/packages/go-common/logger"
	"github.com/icofcucam/naditos/packages/go-common/server"
)

func main() {
	cfg := config.MustLoad("insurance", 8004)
	log := logger.New(cfg.LogLevel)
	mux := http.NewServeMux()
	mux.HandleFunc("GET /v1/insurance/verify", func(w http.ResponseWriter, r *http.Request) {
		httpx.WriteJSON(w, 200, map[string]any{
			"status":   "stub",
			"provider": "n/a",
			"message":  "Phase-2: connect to national insurance verification provider",
		})
	})
	if err := server.Run(context.Background(), log, cfg.Port, mux); err != nil {
		log.Error("server exited", "err", err)
	}
}
