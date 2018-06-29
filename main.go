package main

import (
	"github.com/flachnetz/startup"
	"github.com/flachnetz/startup/startup_http"
	"github.com/flachnetz/startup/startup_metrics"
	"github.com/flachnetz/startup/startup_postgres"
	"github.com/gorilla/handlers"
	"github.com/julienschmidt/httprouter"
	"net/http"
)

func main() {
	var opts struct {
		Base     startup.BaseOptions
		Metrics  startup_metrics.MetricsOptions
		Postgres startup_postgres.PostgresOptions
		HTTP     startup_http.HTTPOptions
	}

	opts.Metrics.Inputs.MetricsPrefix = "kv"
	opts.Postgres.Inputs.Initializer = startup_postgres.
		DefaultMigration("kv_migration")

	startup.MustParseCommandLine(&opts)

	db := opts.Postgres.Connection()
	defer db.Close()

	api := API{kv: KVStore{db: db}}

	opts.HTTP.Serve(startup_http.Config{
		Name: "kv",
		Routing: func(router *httprouter.Router) http.Handler {
			api.RegisterTo(router)
			compress := handlers.CompressHandler(router)

			return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Cache-Control", "no-cache, private")
				compress.ServeHTTP(w, r)
			})
		},
	})
}
