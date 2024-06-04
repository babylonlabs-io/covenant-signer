package metrics

import (
	"net/http"
	_ "net/http/pprof"
	"regexp"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/rs/zerolog/log"
)

func Start(addr string, reg *prometheus.Registry) {
	go start(addr, reg)
}

func start(addr string, reg *prometheus.Registry) {
	// Add Go module build info.
	reg.MustRegister(collectors.NewBuildInfoCollector())
	reg.MustRegister(collectors.NewGoCollector(
		collectors.WithGoCollectorRuntimeMetrics(collectors.GoRuntimeMetricsRule{Matcher: regexp.MustCompile("/.*")})),
	)

	// Expose the registered metrics via HTTP.
	http.Handle("/metrics", promhttp.HandlerFor(
		reg,
		promhttp.HandlerOpts{
			// Opt into OpenMetrics to support exemplars.
			EnableOpenMetrics: true,
		},
	))

	err := http.ListenAndServe(addr, nil)
	if err != nil {
		log.Error().Err(err).Msg("failed to start metrics server")
	}
}
