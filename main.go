package main

import (
	"encoding/csv"
	"flag"
	"fmt"
	"log"
	"net/http"
	"regexp"
	"strconv"
	"strings"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

var (
	listen = flag.String("listen", ":9055", "Host and port to listen on")
)

func readInformation(address string) (map[string]string, error) {
	information := map[string]string{}

	url := fmt.Sprintf("http://%s/etc/mnt_info.csv", address)
	response, err := http.Get(url)
	if err != nil {
		return information, err
	}
	defer response.Body.Close()

	records, err := csv.NewReader(response.Body).ReadAll()
	if err != nil {
		return information, err
	}

	for index, name := range records[0] {
		name = strings.ToLower(strings.Replace(name, "%", "percent", 1))
		name = regexp.MustCompile(`\W+`).ReplaceAllString(name, "_")
		information[name] = records[1][index]
	}

	return information, nil
}

func collectMetrics(address string, registry *prometheus.Registry) {
	success := prometheus.NewGauge(prometheus.GaugeOpts{
		Name:        "brother_success",
		Help:        "Indicates if the last scrape was successful (1) or not (0).",
		ConstLabels: map[string]string{"host": address},
	})
	registry.MustRegister(success)

	information, err := readInformation(address)
	if err != nil {
		success.Set(0)
		log.Printf("Error collecting data for %s: %v", address, err)
		return
	}

	success.Set(1)

	for name, value := range information {
		floatValue, err := strconv.ParseFloat(value, 64)
		if err == nil {
			metric := prometheus.NewGauge(prometheus.GaugeOpts{
				Name:        fmt.Sprintf("brother_%s", name),
				Help:        fmt.Sprintf("Metric %s for Brother printer", name),
				ConstLabels: map[string]string{"host": address},
			})
			metric.Set(floatValue)
			registry.MustRegister(metric)
		}
	}
}

func main() {
	flag.Parse()

	http.HandleFunc("/metrics", func(response http.ResponseWriter, request *http.Request) {
		host := request.URL.Query().Get("host")
		if host == "" {
			http.Error(response, "Query parameter `host` is required", http.StatusBadRequest)
			return
		}

		registry := prometheus.NewRegistry()
		collectMetrics(host, registry)

		promhttp.HandlerFor(
			registry,
			promhttp.HandlerOpts{},
		).ServeHTTP(response, request)
	})

	log.Printf("Starting to listen on %s", *listen)
	log.Fatal(http.ListenAndServe(*listen, nil))
}
