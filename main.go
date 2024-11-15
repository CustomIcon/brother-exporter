package main

import (
	"encoding/csv"
	"flag"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"regexp"
	"strconv"
	"strings"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

var (
	listen     = flag.String("listen", ":9055", "Host and port to listen on")
	configPath = flag.String("config", "/etc/printers.yml", "Path to the printers.yml configuration file")
	printerIPs []string
)

func loadPrinterIPs(configFile string) error {
	data, err := ioutil.ReadFile(configFile)
	if err != nil {
		return fmt.Errorf("could not read printers.yml file: %v", err)
	}
	content := string(data)
	if !strings.Contains(content, "printers:") {
		return fmt.Errorf("printers section not found in the configuration file")
	}
	lines := strings.Split(content, "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "- ") {
			ip := strings.TrimPrefix(line, "- ")
			printerIPs = append(printerIPs, ip)
		}
	}

	if len(printerIPs) == 0 {
		return fmt.Errorf("no printer IPs found in the printers.yml file")
	}

	return nil
}

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
	err := loadPrinterIPs(*configPath)
	if err != nil {
		log.Fatalf("Error loading printer IPs from %s: %v", *configPath, err)
	}

	http.HandleFunc("/metrics", func(response http.ResponseWriter, request *http.Request) {
		registry := prometheus.NewRegistry()
		for _, host := range printerIPs {
			collectMetrics(host, registry)
		}
		promhttp.HandlerFor(
			registry,
			promhttp.HandlerOpts{},
		).ServeHTTP(response, request)
	})

	log.Printf("Starting to listen on %s with config file %s", *listen, *configPath)
	log.Fatal(http.ListenAndServe(*listen, nil))
}
