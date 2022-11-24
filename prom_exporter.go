package main

import (
	"C"
	"net/http"
	"unsafe"

	"github.com/fluent/fluent-bit-go/output"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)
import (
	"context"
	"fmt"
	"strings"
	"sync"
)

type myServer struct {
	wg  *sync.WaitGroup
	srv *http.Server
}

type myCollector struct {
	metrics map[string]*prometheus.Desc
	buff    chan cpuMetrics
}

type cpuMetrics struct {
	cpu   string
	mode  string
	value float64
}

func NewMyCollector() *myCollector {
	return &myCollector{
		metrics: map[string]*prometheus.Desc{
			"cpu": prometheus.NewDesc(
				"cpu",
				"Collect CPU usage",
				[]string{"cpu", "mode"}, nil,
			),
		},
		buff: make(chan cpuMetrics),
	}
}

func (collector *myCollector) Describe(ch chan<- *prometheus.Desc) {
	for _, desc := range collector.metrics {
		ch <- desc
	}
}

func (collector *myCollector) Collect(ch chan<- prometheus.Metric) {

	for _, desc := range collector.metrics {
		select {
		case metric := <-collector.buff:
			fmt.Println(metric.cpu, metric.mode, metric.value)
			ch <- prometheus.MustNewConstMetric(desc, prometheus.GaugeValue, metric.value, metric.cpu, metric.mode)
		default:
			return
		}
	}

}

var collector = NewMyCollector()
var server = myServer{}

func startHttpServer(wg *sync.WaitGroup, reg *prometheus.Registry) *http.Server {
	srv := &http.Server{Addr: ":8989"}

	http.Handle("/metrics", promhttp.HandlerFor(
		reg,
		promhttp.HandlerOpts{
			EnableOpenMetrics: true,
			Registry:          reg,
		},
	))

	go func() {
		defer wg.Done()
		if err := srv.ListenAndServe(); err != http.ErrServerClosed {
			fmt.Println("ListenAndServe():", err)
		}
	}()

	return srv
}

func NewExporter() {
	reg := prometheus.NewRegistry()
	reg.MustRegister(collector)

	server.wg = &sync.WaitGroup{}
	server.wg.Add(1)
	server.srv = startHttpServer(server.wg, reg)
}

//export FLBPluginRegister
func FLBPluginRegister(def unsafe.Pointer) int {
	return output.FLBPluginRegister(def, "promexporter", "Prometheus Exporter")
}

//export FLBPluginInit
func FLBPluginInit(plugin unsafe.Pointer) int {
	field1 := output.FLBPluginConfigKey(plugin, "field1")
	field2 := output.FLBPluginConfigKey(plugin, "field2")
	fmt.Println(field1, field2)
	NewExporter()
	return output.FLB_OK
}

//export FLBPluginFlushCtx
func FLBPluginFlushCtx(ctx, data unsafe.Pointer, length C.int, tag *C.char) int {
	dec := output.NewDecoder(data, int(length))
	for {
		ret, _, record := output.GetRecord(dec)
		fmt.Println("flush record:", record)
		if ret != 0 {
			break
		}

		for k, v := range record {
			recordArray := strings.Split(k.(string), ".")
			m := cpuMetrics{}
			if len(recordArray) == 2 {
				m.cpu = recordArray[0]
				m.mode = recordArray[1]
				m.value = v.(float64)
			} else {
				m.cpu = "ALL"
				m.mode = recordArray[0]
				m.value = v.(float64)
			}
			collector.buff <- m
		}
	}

	return output.FLB_OK
}

//export FLBPluginExit
func FLBPluginExit() int {
	if err := server.srv.Shutdown(context.TODO()); err != nil {
		panic(err)
	}

	close(collector.buff)
	server.wg.Wait()

	return output.FLB_OK
}

func main() {
}
