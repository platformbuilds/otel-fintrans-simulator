package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"math/rand"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"crypto/tls"
	"net/http"
	"net/url"
	"strings"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/exporters/otlp/otlplog/otlploggrpc"
	otlploghttp "go.opentelemetry.io/otel/exporters/otlp/otlplog/otlploghttp"
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetricgrpc"
	otlpmetrichttp "go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetrichttp"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	otlptracehttp "go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	stdoutmetric "go.opentelemetry.io/otel/exporters/stdout/stdoutmetric"
	stdouttrace "go.opentelemetry.io/otel/exporters/stdout/stdouttrace"
	"go.opentelemetry.io/otel/log/global"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/propagation"
	sdklog "go.opentelemetry.io/otel/sdk/log"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.26.0"
	"go.opentelemetry.io/otel/trace"
	"go.uber.org/zap"
	"google.golang.org/grpc"
	grpccreds "google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"
)

const (
	currencyINR = "INR"
)

type FailureMode string

const (
	FailureModeNone       FailureMode = "none"
	FailureModeKafka      FailureMode = "kafka"
	FailureModeCassandra  FailureMode = "cassandra"
	FailureModeKeyDB      FailureMode = "keydb"
	FailureModeAPIGateway FailureMode = "api-gateway"
	FailureModeTPS        FailureMode = "tps"
	FailureModeMixed      FailureMode = "mixed"
)

type Simulator struct {
	tracer         trace.Tracer
	meter          metric.Meter
	logger         *zap.Logger
	transactions   int
	failureMode    FailureMode
	failureRate    float64
	minAmountPaise int64
	maxAmountPaise int64
	concurrency    int

	// Time series configuration
	timeWindowDuration time.Duration // Total duration to spread data over
	dataInterval       time.Duration // Time between data points
	startTimeOffset    time.Duration // How far back to start (e.g., -15m means 15 minutes ago)

	rng *rand.Rand

	// Metric instruments
	transactionsTotal        metric.Int64Counter
	transactionsFailedTotal  metric.Int64Counter
	dbOpsTotal               metric.Int64Counter
	kafkaProduceTotal        metric.Int64Counter
	kafkaConsumeTotal        metric.Int64Counter
	transactionLatency       metric.Float64Histogram
	transactionLatencyBucket metric.Float64Counter
	transactionLatencySum    metric.Float64Counter
	transactionLatencyCount  metric.Int64Counter
	dbLatency                metric.Float64Histogram
	dbLatencyBucket          metric.Float64Counter
	dbLatencySum             metric.Float64Counter
	dbLatencyCount           metric.Int64Counter
	transactionAmountSum     metric.Int64Counter
	transactionAmountCount   metric.Int64Counter
	kafkaProduceLatency      metric.Float64Histogram
	kafkaConsumeLatency      metric.Float64Histogram

	// Additional instruments for KPI coverage
	jvmMemoryUsed          metric.Float64UpDownCounter
	jvmMemoryMax           metric.Float64UpDownCounter
	jvmGCPauseSecondsSum   metric.Float64Counter
	jvmGCPauseSecondsCount metric.Int64Counter

	tomcatThreadsBusy     metric.Int64UpDownCounter
	tomcatThreadsMax      metric.Int64UpDownCounter
	tomcatThreadsQueueSec metric.Float64UpDownCounter

	processCPUUsage      metric.Float64UpDownCounter
	processUptimeSeconds metric.Float64UpDownCounter
	processFilesOpen     metric.Int64UpDownCounter
	processFilesMax      metric.Int64UpDownCounter

	hikariConnectionsActive metric.Int64UpDownCounter
	hikariConnectionsMax    metric.Int64UpDownCounter

	// Redis / KeyDB
	redisMemoryUsed       metric.Float64UpDownCounter
	redisMemoryMax        metric.Float64UpDownCounter
	redisKeyspaceHits     metric.Int64Counter
	redisKeyspaceMisses   metric.Int64Counter
	redisEvictedKeys      metric.Int64Counter
	redisConnectedClients metric.Int64UpDownCounter

	// Redis replication offsets (master/slave)
	redisMasterReplOffset metric.Int64UpDownCounter
	redisSlaveReplOffset  metric.Int64UpDownCounter

	// Kafka JMX-like metrics
	kafkaUnderReplicated metric.Int64UpDownCounter
	kafkaRequestsPerSec  metric.Float64Counter
	kafkaBytesInPerSec   metric.Float64Counter
	kafkaRequestErrors   metric.Int64Counter
	kafkaISRChanges      metric.Float64Counter

	// Node exporter / infra
	nodeLoad1              metric.Float64UpDownCounter
	nodeMemoryAvailable    metric.Float64UpDownCounter
	nodeMemoryTotal        metric.Float64UpDownCounter
	nodeFilesystemAvail    metric.Float64UpDownCounter
	nodeFilesystemSize     metric.Float64UpDownCounter
	nodeDiskIOTimeSeconds  metric.Float64Counter
	nodeNetworkLatencyMs   metric.Float64UpDownCounter
	nodeNetworkPacketDrops metric.Int64Counter
	nodeContextSwitches    metric.Float64Counter

	// internal state for gauge-like values (kept so we can use UpDown counters)
	stateLock                sync.Mutex
	stateJvmUsed             float64
	stateJvmMax              float64
	stateTomcatBusy          int64
	stateTomcatMax           int64
	stateTomcatQueue         float64
	stateCPU                 float64
	stateProcessUptime       float64
	stateFilesOpen           int64
	stateFilesMax            int64
	stateHikariActive        int64
	stateHikariMax           int64
	stateRedisUsed           float64
	stateRedisMasterOffset   int64
	stateRedisSlaveOffset    int64
	stateRedisMasterReplMult float64
	stateRedisSlaveReplMult  float64
	stateRedisMax            float64
	stateRedisClients        int64
	stateKafkaURP            int64
	stateNodeLoad1           float64
	stateNodeMemAvail        float64
	stateNodeMemTotal        float64
	stateFsAvail             float64
	stateFsSize              float64
	stateNodeNetworkLatency  float64

	// scenario scheduler
	scenarioSched *scenarioScheduler

	// per-target multipliers/state applied by active scenarios
	stateDbLatencyMult   float64
	stateGCPauseMult     float64
	stateFailureMult     float64
	stateTomcatLoadMult  float64
	stateRedisMemoryMult float64
	stateKafkaURPMult    float64
	// additional multiplier to simulate reduced or increased kafka throughput independent of errors
	stateKafkaReqsMult float64
	// per-subsystem scenario multipliers used to simulate infra/hardware faults
	// e.g. disk failures causing kafka errors, memory faults causing KeyDB failures
	stateKafkaErrorMult float64
	stateKeydbFailMult  float64
	// network-related scenario multipliers
	stateNetworkLatencyMult float64
	stateNetworkDropMult    float64
	// runtime configuration
	telemCfg     TelemetryConfig
	failureSched *failureScheduler
	// histogram bucket boundaries (predefined)
	transactionLatencyBuckets []float64
	dbLatencyBuckets          []float64
}

type TransactionResult struct {
	TransactionID string
	AmountPaise   int64
	CustomerID    string
	Channel       string
	FinalStatus   string
	ErrorReason   string
}

func main() {
	var (
		transactions       = flag.Int("transactions", 100, "Number of transactions to simulate")
		failureModeStr     = flag.String("failure-mode", "mixed", "Failure mode: none, kafka, cassandra, keydb, api-gateway, tps, mixed")
		failureRate        = flag.Float64("failure-rate", 0.1, "Failure rate (0.0-1.0)")
		minAmountPaise     = flag.Int64("min-amount-paisa", 10000, "Minimum transaction amount in paise (₹100.00)")
		maxAmountPaise     = flag.Int64("max-amount-paisa", 1000000, "Maximum transaction amount in paise (₹10,000.00)")
		concurrency        = flag.Int("concurrency", 10, "Number of concurrent transactions")
		timeWindowStr      = flag.String("time-window", "0s", "Time window to spread data over (e.g., 15m, 1h, 0s=instant)")
		dataIntervalStr    = flag.String("data-interval", "30s", "Time interval between data points (e.g., 10s, 1m)")
		startTimeOffsetStr = flag.String("start-time-offset", "0s", "Start time offset from now (e.g., -15m for 15 minutes ago, 0s=now)")
		configPath         = flag.String("config", "", "Path to optional simulator YAML configuration file (supports telemetry names, label sets, failure bursts and 'scenarios' for correlated injections)")
		randSeed           = flag.Int64("rand-seed", 0, "Optional seed for RNG to make runs deterministic")
		logOutput          = flag.String("log-output", "nop", "Logger output: 'nop' (default) or 'stdout')")
		metricIntervalStr  = flag.String("signal-time-interval", "15s", "Interval between metric exports (e.g., 15s, 1m)")
	)
	flag.Parse()

	failureMode := FailureMode(*failureModeStr)
	if failureMode != FailureModeNone && failureMode != FailureModeKafka &&
		failureMode != FailureModeCassandra && failureMode != FailureModeKeyDB &&
		failureMode != FailureModeAPIGateway && failureMode != FailureModeTPS &&
		failureMode != FailureModeMixed {
		log.Fatalf("Invalid failure mode: %s", failureMode)
	}

	if *failureRate < 0 || *failureRate > 1 {
		log.Fatalf("Failure rate must be between 0.0 and 1.0")
	}

	// Parse time durations
	timeWindowDuration, err := time.ParseDuration(*timeWindowStr)
	if err != nil {
		log.Fatalf("Invalid time-window: %v", err)
	}

	dataInterval, err := time.ParseDuration(*dataIntervalStr)
	if err != nil {
		log.Fatalf("Invalid data-interval: %v", err)
	}

	startTimeOffset, err := time.ParseDuration(*startTimeOffsetStr)
	if err != nil {
		log.Fatalf("Invalid start-time-offset: %v", err)
	}

	// Create a cancellable context that terminates on SIGINT/SIGTERM for graceful shutdown
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	// Load optional config file early so it can influence telemetry outputs
	cfg, err := LoadConfig(*configPath)
	if err != nil {
		log.Fatalf("failed to load config: %v", err)
	}

	// determine telemetry outputs from configuration (simulator-config.yaml)
	telemetryOutputs := ""
	// flag wasn't added yet - read from env var or config
	// We'll allow a flag later; for now, prefer config if present
	if len(cfg.Telemetry.Outputs) > 0 {
		// map outputs into single keyword: "otlp", "stdout", or "both"
		hasOTLP := false
		hasStd := false
		for _, o := range cfg.Telemetry.Outputs {
			switch o {
			case "otlp":
				hasOTLP = true
			case "stdout":
				hasStd = true
			}
		}
		if hasOTLP && hasStd {
			telemetryOutputs = "both"
		} else if hasStd {
			telemetryOutputs = "stdout"
		} else if hasOTLP {
			telemetryOutputs = "otlp"
		}
	}

	// If telemetryOutputs includes stdout and the user hasn't requested a logger output explicitly,
	// make logs print to stdout by default so traces/metrics/logs are all visible.
	if telemetryOutputs == "both" || telemetryOutputs == "stdout" {
		if *logOutput == "nop" {
			*logOutput = "stdout"
		}
	}

	// derive endpoint and insecure option from config (defaults if absent)
	endpoint := "localhost:4317"
	insecure := true
	if cfg != nil {
		if cfg.Telemetry.Endpoint != "" {
			endpoint = cfg.Telemetry.Endpoint
		}
		insecure = cfg.Telemetry.Insecure
	}
	// determine skipVerify early and validate telemetry config and log helpful warnings
	skipVerify := false
	if cfg != nil {
		skipVerify = cfg.Telemetry.SkipTLSVerify
	}
	// validate telemetry config and log any helpful warnings for inconsistent combos
	for _, w := range validateTelemetryConfig(endpoint, insecure, skipVerify) {
		log.Printf("Config warning: %s", w)
	}

	// parse metric interval for periodic reader
	metricInterval, err := time.ParseDuration(*metricIntervalStr)
	if err != nil {
		log.Fatalf("Invalid signal-time-interval: %v", err)
	}

	shutdown, err := initOTel(ctx, endpoint, insecure, skipVerify, telemetryOutputs, metricInterval)
	if err != nil {
		log.Fatalf("Failed to initialize OpenTelemetry: %v", err)
	}
	defer func() {
		if err := shutdown(ctx); err != nil {
			log.Printf("Failed to shutdown OpenTelemetry: %v", err)
		}
	}()

	// Initialize per-simulator RNG. Use config failure seed if present (deterministic),
	// otherwise use CLI rand-seed if provided, else time-based seed.

	// Create simulator
	sim := &Simulator{
		tracer:             otel.Tracer("fintrans-simulator"),
		meter:              otel.Meter("fintrans-simulator"),
		logger:             zap.NewNop(), // Will be set after OTel init
		transactions:       *transactions,
		failureMode:        failureMode,
		failureRate:        *failureRate,
		minAmountPaise:     *minAmountPaise,
		maxAmountPaise:     *maxAmountPaise,
		concurrency:        *concurrency,
		timeWindowDuration: timeWindowDuration,
		dataInterval:       dataInterval,
		startTimeOffset:    startTimeOffset,
	}

	// Initialize RNG for simulator
	if cfg != nil && cfg.Failure.Seed != nil {
		sim.rng = rand.New(rand.NewSource(*cfg.Failure.Seed))
	} else if *randSeed != 0 {
		sim.rng = rand.New(rand.NewSource(*randSeed))
	} else {
		sim.rng = rand.New(rand.NewSource(time.Now().UnixNano()))
	}

	if cfg != nil {
		sim.telemCfg = cfg.Telemetry

		// If the config contains a failure section, and has a rate/bursts/mode, build scheduler
		if cfg.Failure.Rate > 0 || len(cfg.Failure.Bursts) > 0 || cfg.Failure.Mode != "" {
			fs, err := newFailureScheduler(cfg.Failure, time.Now().Add(sim.startTimeOffset))
			if err != nil {
				log.Fatalf("failed to create failure scheduler: %v", err)
			}
			sim.failureSched = fs

			// deterministic behavior for failure scheduler handled inside newFailureScheduler
		}

		// create scenario scheduler if scenarios defined
		if len(cfg.Failure.Scenarios) > 0 {
			ss, err := newScenarioScheduler(cfg.Failure, time.Now().Add(sim.startTimeOffset))
			if err != nil {
				log.Fatalf("failed to create scenario scheduler: %v", err)
			}
			sim.scenarioSched = ss
		}
	}

	// Initialize metrics
	if err := sim.initMetrics(ctx); err != nil {
		log.Fatalf("Failed to initialize metrics: %v", err)
	}

	// Initialize logger with OTel bridge
	sim.initLogger(*logOutput)

	log.Printf("Starting financial transaction simulator with %d transactions, concurrency: %d, failure mode: %s, rate: %.2f",
		*transactions, *concurrency, failureMode, *failureRate)

	if timeWindowDuration > 0 {
		log.Printf("Time series mode: window=%v, interval=%v, start_offset=%v",
			timeWindowDuration, dataInterval, startTimeOffset)
	} else {
		log.Printf("Instant mode: all transactions at current time")
	}

	// Run simulation
	sim.runSimulation(ctx)

	log.Println("Simulation completed")
}

func initOTel(ctx context.Context, endpoint string, insecureConn bool, skipVerify bool, outputs string, metricInterval time.Duration) (func(context.Context) error, error) {
	res, err := resource.New(ctx,
		resource.WithAttributes(
			semconv.ServiceName("fintrans-simulator"),
			semconv.ServiceVersion("1.0.0"),
		),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create resource: %w", err)
	}

	// Choose protocol based on endpoint string or default port
	protocol := "grpc"
	epHost := endpoint
	if u, err := url.Parse(endpoint); err == nil {
		if u.Scheme == "http" || u.Scheme == "https" {
			protocol = "http"
			epHost = u.Host
		}
	}
	if protocol != "http" {
		// fall back to port-based heuristic
		if strings.Contains(endpoint, ":4318") {
			protocol = "http"
		}
	}

	// gRPC dial options (for gRPC exporters)
	opts := []grpc.DialOption{}
	if protocol == "grpc" {
		if insecureConn {
			opts = append(opts, grpc.WithTransportCredentials(insecure.NewCredentials()))
		} else if skipVerify {
			// Use TLS but skip verification
			creds := grpccreds.NewTLS(&tls.Config{InsecureSkipVerify: true})
			opts = append(opts, grpc.WithTransportCredentials(creds))
		} else {
			// Use default TLS
			creds := grpccreds.NewClientTLSFromCert(nil, "")
			opts = append(opts, grpc.WithTransportCredentials(creds))
		}
	}

	// Determine if we should set up OTLP and/or stdout exporters
	wantOTLP := outputs == "" || outputs == "otlp" || outputs == "both"
	wantStdout := outputs == "stdout" || outputs == "both"

	// Trace exporter(s)
	var traceExporters []sdktrace.SpanProcessor
	var tracerProvider *sdktrace.TracerProvider
	if wantOTLP {
		if protocol == "grpc" {
			traceOpts := []otlptracegrpc.Option{
				otlptracegrpc.WithEndpoint(epHost),
				otlptracegrpc.WithDialOption(opts...),
			}
			if insecureConn {
				traceOpts = append(traceOpts, otlptracegrpc.WithInsecure())
			}
			traceExporter, err := otlptracegrpc.New(ctx, traceOpts...)
			if err != nil {
				return nil, fmt.Errorf("failed to create trace exporter: %w", err)
			}
			traceExporters = append(traceExporters, sdktrace.NewBatchSpanProcessor(traceExporter))
		} else {
			// HTTP exporter
			traceHTTPOpts := []otlptracehttp.Option{otlptracehttp.WithEndpoint(epHost)}
			if insecureConn {
				traceHTTPOpts = append(traceHTTPOpts, otlptracehttp.WithInsecure())
			}
			if !insecureConn && skipVerify {
				// create client that skips TLS verification
				client := &http.Client{Transport: &http.Transport{TLSClientConfig: &tls.Config{InsecureSkipVerify: true}}}
				traceHTTPOpts = append(traceHTTPOpts, otlptracehttp.WithHTTPClient(client))
			}
			traceExporter, err := otlptracehttp.New(ctx, traceHTTPOpts...)
			if err != nil {
				return nil, fmt.Errorf("failed to create HTTP trace exporter: %w", err)
			}
			traceExporters = append(traceExporters, sdktrace.NewBatchSpanProcessor(traceExporter))
		}
	}
	if wantStdout {
		// stdout trace exporter (pretty)
		stExporter, err := stdouttrace.New(stdouttrace.WithPrettyPrint())
		if err != nil {
			return nil, fmt.Errorf("failed to create stdout trace exporter: %w", err)
		}
		traceExporters = append(traceExporters, sdktrace.NewBatchSpanProcessor(stExporter))
	}

	tracerProvider = sdktrace.NewTracerProvider(
		sdktrace.WithResource(res),
	)
	// Add processors to tracer provider
	for _, p := range traceExporters {
		tracerProvider.RegisterSpanProcessor(p)
	}
	otel.SetTracerProvider(tracerProvider)

	// Metric exporter
	var metricReaders []sdkmetric.Reader
	if wantOTLP {
		if protocol == "grpc" {
			metricOpts := []otlpmetricgrpc.Option{
				otlpmetricgrpc.WithEndpoint(epHost),
				otlpmetricgrpc.WithDialOption(opts...),
			}
			if insecureConn {
				metricOpts = append(metricOpts, otlpmetricgrpc.WithInsecure())
			}
			metricExporter, err := otlpmetricgrpc.New(ctx, metricOpts...)
			if err != nil {
				return nil, fmt.Errorf("failed to create metric exporter: %w", err)
			}
			metricReaders = append(metricReaders, sdkmetric.NewPeriodicReader(metricExporter, sdkmetric.WithInterval(metricInterval)))
		} else {
			metricHTTPOpts := []otlpmetrichttp.Option{otlpmetrichttp.WithEndpoint(epHost)}
			if insecureConn {
				metricHTTPOpts = append(metricHTTPOpts, otlpmetrichttp.WithInsecure())
			}
			if !insecureConn && skipVerify {
				client := &http.Client{Transport: &http.Transport{TLSClientConfig: &tls.Config{InsecureSkipVerify: true}}}
				metricHTTPOpts = append(metricHTTPOpts, otlpmetrichttp.WithHTTPClient(client))
			}
			metricExporter, err := otlpmetrichttp.New(ctx, metricHTTPOpts...)
			if err != nil {
				return nil, fmt.Errorf("failed to create HTTP metric exporter: %w", err)
			}
			metricReaders = append(metricReaders, sdkmetric.NewPeriodicReader(metricExporter, sdkmetric.WithInterval(metricInterval)))
		}
	}

	if wantStdout {
		// stdout metric exporter
		// Use the stdout exporter wrapped with a periodic reader
		smExporter, err := stdoutmetric.New(stdoutmetric.WithPrettyPrint())
		if err != nil {
			return nil, fmt.Errorf("failed to create stdout metric exporter: %w", err)
		}
		metricReaders = append(metricReaders, sdkmetric.NewPeriodicReader(smExporter, sdkmetric.WithInterval(metricInterval)))
	}

	// Build options for meter provider so we can register multiple readers
	meterOpts := []sdkmetric.Option{sdkmetric.WithResource(res)}
	for _, r := range metricReaders {
		meterOpts = append(meterOpts, sdkmetric.WithReader(r))
	}
	meterProvider := sdkmetric.NewMeterProvider(meterOpts...)
	otel.SetMeterProvider(meterProvider)

	// Log exporter
	var logOptsGRPC []otlploggrpc.Option
	var logHTTPOpts []otlploghttp.Option
	if protocol == "grpc" {
		logOptsGRPC = []otlploggrpc.Option{
			otlploggrpc.WithEndpoint(epHost),
			otlploggrpc.WithDialOption(opts...),
		}
		if insecureConn {
			logOptsGRPC = append(logOptsGRPC, otlploggrpc.WithInsecure())
		}
	} else {
		logHTTPOpts = []otlploghttp.Option{otlploghttp.WithEndpoint(epHost)}
		if insecureConn {
			logHTTPOpts = append(logHTTPOpts, otlploghttp.WithInsecure())
		}
		if !insecureConn && skipVerify {
			client := &http.Client{Transport: &http.Transport{TLSClientConfig: &tls.Config{InsecureSkipVerify: true}}}
			logHTTPOpts = append(logHTTPOpts, otlploghttp.WithHTTPClient(client))
		}
	}
	// log exporter (created only if OTLP chosen below)

	// For logs: if OTLP requested create the OTLP exporter; for stdout we rely on simulator logger (stdout) for simpler behaviour.
	var logProvider *sdklog.LoggerProvider
	if wantOTLP {
		if protocol == "grpc" {
			logExporter, err := otlploggrpc.New(ctx, logOptsGRPC...)
			if err != nil {
				return nil, fmt.Errorf("failed to create log exporter: %w", err)
			}
			logProvider = sdklog.NewLoggerProvider(
				sdklog.WithProcessor(sdklog.NewBatchProcessor(logExporter)),
				sdklog.WithResource(res),
			)
			global.SetLoggerProvider(logProvider)
		} else {
			logExporter, err := otlploghttp.New(ctx, logHTTPOpts...)
			if err != nil {
				return nil, fmt.Errorf("failed to create HTTP log exporter: %w", err)
			}
			logProvider = sdklog.NewLoggerProvider(
				sdklog.WithProcessor(sdklog.NewBatchProcessor(logExporter)),
				sdklog.WithResource(res),
			)
			global.SetLoggerProvider(logProvider)
		}
	}

	otel.SetTextMapPropagator(propagation.TraceContext{})

	return func(ctx context.Context) error {
		var errs []error
		if err := tracerProvider.Shutdown(ctx); err != nil {
			errs = append(errs, err)
		}
		if err := meterProvider.Shutdown(ctx); err != nil {
			errs = append(errs, err)
		}
		if logProvider != nil {
			if err := logProvider.Shutdown(ctx); err != nil {
				errs = append(errs, err)
			}
		}
		if len(errs) > 0 {
			return fmt.Errorf("shutdown errors: %v", errs)
		}
		return nil
	}, nil
}

// validateTelemetryConfig returns a slice of human-friendly warnings
// describing potentially inconsistent telemetry configuration passed from YAML.
func validateTelemetryConfig(endpoint string, insecure bool, skipVerify bool) []string {
	var warnings []string

	if endpoint == "" {
		return warnings
	}

	// parse endpoint for scheme detection
	u, err := url.Parse(endpoint)
	hasScheme := err == nil && u.Scheme != ""

	// determine protocol heuristic
	usesHTTP := false
	if hasScheme {
		usesHTTP = (u.Scheme == "http" || u.Scheme == "https")
	} else if strings.Contains(endpoint, ":4318") {
		usesHTTP = true
	}

	// HTTP vs gRPC + insecure/skipVerify checks
	if usesHTTP {
		// If the endpoint explicitly uses an http scheme but specifies the
		// conventional gRPC port (4317) that's likely a misconfiguration —
		// warn the user and suggest the canonical gRPC style (no scheme
		// e.g. localhost:4317) or using port 4318 for OTLP/HTTP.
		if hasScheme && u.Scheme == "http" && u.Port() == "4317" {
			warnings = append(warnings, "endpoint uses http:// on port 4317 — port 4317 is conventionally used for OTLP/gRPC; use 'localhost:4317' (no scheme) for gRPC or use http(s) on port 4318 for OTLP/HTTP")
		}
		if hasScheme && u.Scheme == "http" && !insecure {
			warnings = append(warnings, "endpoint uses http:// scheme but telemetry.insecure=false — http is plaintext; set insecure=true or use https:// for TLS")
		}
		if hasScheme && u.Scheme == "https" && insecure {
			warnings = append(warnings, "endpoint uses https:// but telemetry.insecure=true — insecure=true requests plaintext over TLS endpoint; set insecure=false for TLS or use http:// for plaintext")
		}
	} else {
		// gRPC heuristic
		if insecure && skipVerify {
			warnings = append(warnings, "telemetry.skip_tls_verify is ignored when telemetry.insecure=true (plaintext)")
		}
	}

	// skipVerify is only meaningful when TLS is enabled
	if skipVerify && insecure {
		warnings = append(warnings, "telemetry.skip_tls_verify=true has no effect when telemetry.insecure=true (plaintext)")
	}

	return warnings
}

func (s *Simulator) initMetrics(ctx context.Context) error {
	var err error

	txTotalName := "transactions_total"
	if s.telemCfg.MetricNames.TransactionsTotal != "" {
		txTotalName = s.telemCfg.MetricNames.TransactionsTotal
	}
	s.transactionsTotal, err = s.meter.Int64Counter(txTotalName,
		metric.WithDescription("Total number of transactions"))
	if err != nil {
		return err
	}

	txFailedName := "transactions_failed_total"
	if s.telemCfg.MetricNames.TransactionsFailed != "" {
		txFailedName = s.telemCfg.MetricNames.TransactionsFailed
	}
	s.transactionsFailedTotal, err = s.meter.Int64Counter(txFailedName,
		metric.WithDescription("Total number of failed transactions"))
	if err != nil {
		return err
	}

	dbOpsName := "db_ops_total"
	if s.telemCfg.MetricNames.DBOpsTotal != "" {
		dbOpsName = s.telemCfg.MetricNames.DBOpsTotal
	}
	s.dbOpsTotal, err = s.meter.Int64Counter(dbOpsName,
		metric.WithDescription("Total database operations"))
	if err != nil {
		return err
	}

	kafkaProduceName := "kafka_produce_total"
	if s.telemCfg.MetricNames.KafkaProduceTotal != "" {
		kafkaProduceName = s.telemCfg.MetricNames.KafkaProduceTotal
	}
	s.kafkaProduceTotal, err = s.meter.Int64Counter(kafkaProduceName,
		metric.WithDescription("Total Kafka produce operations"))
	if err != nil {
		return err
	}

	kafkaConsumeName := "kafka_consume_total"
	if s.telemCfg.MetricNames.KafkaConsumeTotal != "" {
		kafkaConsumeName = s.telemCfg.MetricNames.KafkaConsumeTotal
	}
	s.kafkaConsumeTotal, err = s.meter.Int64Counter(kafkaConsumeName,
		metric.WithDescription("Total Kafka consume operations"))
	if err != nil {
		return err
	}

	txLatencyName := "transaction_latency_seconds"
	if s.telemCfg.MetricNames.TransactionLatency != "" {
		txLatencyName = s.telemCfg.MetricNames.TransactionLatency
	}
	s.transactionLatency, err = s.meter.Float64Histogram(txLatencyName,
		metric.WithDescription("Transaction processing latency"))
	if err != nil {
		return err
	}
	// transaction histogram counters for PromQL-style buckets
	txBucketName := txLatencyName + "_bucket"
	s.transactionLatencyBucket, err = s.meter.Float64Counter(txBucketName,
		metric.WithDescription("Transaction latency buckets (cumulative)"))
	if err != nil {
		return err
	}
	txSumName := txLatencyName + "_sum"
	s.transactionLatencySum, err = s.meter.Float64Counter(txSumName,
		metric.WithDescription("Transaction latency sum (seconds)"))
	if err != nil {
		return err
	}
	txCountName := txLatencyName + "_count"
	s.transactionLatencyCount, err = s.meter.Int64Counter(txCountName,
		metric.WithDescription("Transaction latency count"))
	if err != nil {
		return err
	}
	// default buckets
	s.transactionLatencyBuckets = []float64{0.001, 0.005, 0.01, 0.05, 0.1, 0.5, 1, 2, 5, 10}

	dbLatencyName := "db_latency_seconds"
	if s.telemCfg.MetricNames.DBLatency != "" {
		dbLatencyName = s.telemCfg.MetricNames.DBLatency
	}
	s.dbLatency, err = s.meter.Float64Histogram(dbLatencyName,
		metric.WithDescription("Database operation latency"))
	if err != nil {
		return err
	}
	// db histogram counters
	dBucketName := dbLatencyName + "_bucket"
	s.dbLatencyBucket, err = s.meter.Float64Counter(dBucketName,
		metric.WithDescription("DB latency buckets (cumulative)"))
	if err != nil {
		return err
	}
	dSumName := dbLatencyName + "_sum"
	s.dbLatencySum, err = s.meter.Float64Counter(dSumName,
		metric.WithDescription("DB latency sum (seconds)"))
	if err != nil {
		return err
	}
	dCountName := dbLatencyName + "_count"
	s.dbLatencyCount, err = s.meter.Int64Counter(dCountName,
		metric.WithDescription("DB latency count"))
	if err != nil {
		return err
	}
	// default db buckets (similar but fewer)
	s.dbLatencyBuckets = []float64{0.0005, 0.001, 0.005, 0.01, 0.05, 0.1, 0.5, 1, 2}

	txAmountSumName := "transaction_amount_paisa_sum"
	if s.telemCfg.MetricNames.TransactionAmountSum != "" {
		txAmountSumName = s.telemCfg.MetricNames.TransactionAmountSum
	}
	s.transactionAmountSum, err = s.meter.Int64Counter(txAmountSumName,
		metric.WithDescription("Sum of transaction amounts in paise"))
	if err != nil {
		return err
	}

	txAmountCountName := "transaction_amount_paisa_count"
	if s.telemCfg.MetricNames.TransactionAmountCount != "" {
		txAmountCountName = s.telemCfg.MetricNames.TransactionAmountCount
	}
	s.transactionAmountCount, err = s.meter.Int64Counter(txAmountCountName,
		metric.WithDescription("Count of transactions for amount metrics"))
	if err != nil {
		return err
	}

	// Kafka latency metrics (separate from DB latency)
	kafkaProduceLatencyName := "kafka_produce_latency_seconds"
	s.kafkaProduceLatency, err = s.meter.Float64Histogram(kafkaProduceLatencyName,
		metric.WithDescription("Latency for kafka produce operations"))
	if err != nil {
		return err
	}

	kafkaConsumeLatencyName := "kafka_consume_latency_seconds"
	s.kafkaConsumeLatency, err = s.meter.Float64Histogram(kafkaConsumeLatencyName,
		metric.WithDescription("Latency for kafka consume operations"))
	if err != nil {
		return err
	}

	// --- additional instrument initialization ---
	// JVM
	jvmUsedName := "jvm_memory_used_bytes"
	if s.telemCfg.MetricNames.Additional != nil {
		if n, ok := s.telemCfg.MetricNames.Additional[jvmUsedName]; ok && n != "" {
			jvmUsedName = n
		}
	}
	s.jvmMemoryUsed, err = s.meter.Float64UpDownCounter(jvmUsedName,
		metric.WithDescription("JVM heap memory used in bytes"))
	if err != nil {
		return err
	}

	jvmMaxName := "jvm_memory_max_bytes"
	if s.telemCfg.MetricNames.Additional != nil {
		if n, ok := s.telemCfg.MetricNames.Additional[jvmMaxName]; ok && n != "" {
			jvmMaxName = n
		}
	}
	s.jvmMemoryMax, err = s.meter.Float64UpDownCounter(jvmMaxName,
		metric.WithDescription("JVM heap memory max in bytes"))
	if err != nil {
		return err
	}

	jvmGCSumName := "jvm_gc_pause_seconds_sum"
	if s.telemCfg.MetricNames.Additional != nil {
		if n, ok := s.telemCfg.MetricNames.Additional[jvmGCSumName]; ok && n != "" {
			jvmGCSumName = n
		}
	}
	s.jvmGCPauseSecondsSum, err = s.meter.Float64Counter(jvmGCSumName,
		metric.WithDescription("Total seconds spent in JVM GC"))
	if err != nil {
		return err
	}

	jvmGCCountName := "jvm_gc_pause_seconds_count"
	if s.telemCfg.MetricNames.Additional != nil {
		if n, ok := s.telemCfg.MetricNames.Additional[jvmGCCountName]; ok && n != "" {
			jvmGCCountName = n
		}
	}
	s.jvmGCPauseSecondsCount, err = s.meter.Int64Counter(jvmGCCountName,
		metric.WithDescription("Total number of JVM garbage collections"))
	if err != nil {
		return err
	}

	// Tomcat
	tomcatBusyName := "tomcat_threads_busy_threads"
	if s.telemCfg.MetricNames.Additional != nil {
		if n, ok := s.telemCfg.MetricNames.Additional[tomcatBusyName]; ok && n != "" {
			tomcatBusyName = n
		}
	}
	s.tomcatThreadsBusy, err = s.meter.Int64UpDownCounter(tomcatBusyName,
		metric.WithDescription("Number of busy tomcat threads"))
	if err != nil {
		return err
	}

	tomcatMaxName := "tomcat_threads_config_max_threads"
	if s.telemCfg.MetricNames.Additional != nil {
		if n, ok := s.telemCfg.MetricNames.Additional[tomcatMaxName]; ok && n != "" {
			tomcatMaxName = n
		}
	}
	s.tomcatThreadsMax, err = s.meter.Int64UpDownCounter(tomcatMaxName,
		metric.WithDescription("Configured number of tomcat max threads"))
	if err != nil {
		return err
	}

	tomcatQueueName := "tomcat_threads_queue_seconds"
	if s.telemCfg.MetricNames.Additional != nil {
		if n, ok := s.telemCfg.MetricNames.Additional[tomcatQueueName]; ok && n != "" {
			tomcatQueueName = n
		}
	}
	s.tomcatThreadsQueueSec, err = s.meter.Float64UpDownCounter(tomcatQueueName,
		metric.WithDescription("Queue seconds spent waiting for Tomcat threads"))
	if err != nil {
		return err
	}

	// Process / runtime
	cpuName := "process_cpu_usage"
	if s.telemCfg.MetricNames.Additional != nil {
		if n, ok := s.telemCfg.MetricNames.Additional[cpuName]; ok && n != "" {
			cpuName = n
		}
	}
	s.processCPUUsage, err = s.meter.Float64UpDownCounter(cpuName,
		metric.WithDescription("CPU usage (percent) for process"))
	if err != nil {
		return err
	}

	uptimeName := "process_uptime_seconds"
	if s.telemCfg.MetricNames.Additional != nil {
		if n, ok := s.telemCfg.MetricNames.Additional[uptimeName]; ok && n != "" {
			uptimeName = n
		}
	}
	s.processUptimeSeconds, err = s.meter.Float64UpDownCounter(uptimeName,
		metric.WithDescription("Process uptime in seconds"))
	if err != nil {
		return err
	}

	filesOpenName := "process_files_open_files"
	if s.telemCfg.MetricNames.Additional != nil {
		if n, ok := s.telemCfg.MetricNames.Additional[filesOpenName]; ok && n != "" {
			filesOpenName = n
		}
	}
	s.processFilesOpen, err = s.meter.Int64UpDownCounter(filesOpenName,
		metric.WithDescription("Process open file descriptors"))
	if err != nil {
		return err
	}

	filesMaxName := "process_files_max_files"
	if s.telemCfg.MetricNames.Additional != nil {
		if n, ok := s.telemCfg.MetricNames.Additional[filesMaxName]; ok && n != "" {
			filesMaxName = n
		}
	}
	s.processFilesMax, err = s.meter.Int64UpDownCounter(filesMaxName,
		metric.WithDescription("Process max file descriptors"))
	if err != nil {
		return err
	}

	// Hikari
	hikariActiveName := "hikaricp_connections_active"
	if s.telemCfg.MetricNames.Additional != nil {
		if n, ok := s.telemCfg.MetricNames.Additional[hikariActiveName]; ok && n != "" {
			hikariActiveName = n
		}
	}
	s.hikariConnectionsActive, err = s.meter.Int64UpDownCounter(hikariActiveName,
		metric.WithDescription("Hikari active connections"))
	if err != nil {
		return err
	}

	hikariMaxName := "hikaricp_connections_max"
	if s.telemCfg.MetricNames.Additional != nil {
		if n, ok := s.telemCfg.MetricNames.Additional[hikariMaxName]; ok && n != "" {
			hikariMaxName = n
		}
	}
	s.hikariConnectionsMax, err = s.meter.Int64UpDownCounter(hikariMaxName,
		metric.WithDescription("Hikari max connections"))
	if err != nil {
		return err
	}

	// Redis / KeyDB
	redisUsedName := "redis_memory_used_bytes"
	if s.telemCfg.MetricNames.Additional != nil {
		if n, ok := s.telemCfg.MetricNames.Additional[redisUsedName]; ok && n != "" {
			redisUsedName = n
		}
	}
	s.redisMemoryUsed, err = s.meter.Float64UpDownCounter(redisUsedName,
		metric.WithDescription("Redis/KeyDB memory used bytes"))
	if err != nil {
		return err
	}

	redisMaxName := "redis_memory_max_bytes"
	if s.telemCfg.MetricNames.Additional != nil {
		if n, ok := s.telemCfg.MetricNames.Additional[redisMaxName]; ok && n != "" {
			redisMaxName = n
		}
	}
	s.redisMemoryMax, err = s.meter.Float64UpDownCounter(redisMaxName,
		metric.WithDescription("Redis/KeyDB memory max bytes"))
	if err != nil {
		return err
	}

	redisHitsName := "redis_keyspace_hits_total"
	if s.telemCfg.MetricNames.Additional != nil {
		if n, ok := s.telemCfg.MetricNames.Additional[redisHitsName]; ok && n != "" {
			redisHitsName = n
		}
	}
	s.redisKeyspaceHits, err = s.meter.Int64Counter(redisHitsName,
		metric.WithDescription("Redis keyspace hits"))
	if err != nil {
		return err
	}

	redisMissName := "redis_keyspace_misses_total"
	if s.telemCfg.MetricNames.Additional != nil {
		if n, ok := s.telemCfg.MetricNames.Additional[redisMissName]; ok && n != "" {
			redisMissName = n
		}
	}
	s.redisKeyspaceMisses, err = s.meter.Int64Counter(redisMissName,
		metric.WithDescription("Redis keyspace misses"))
	if err != nil {
		return err
	}

	redisEvictName := "redis_evicted_keys_total"
	if s.telemCfg.MetricNames.Additional != nil {
		if n, ok := s.telemCfg.MetricNames.Additional[redisEvictName]; ok && n != "" {
			redisEvictName = n
		}
	}
	s.redisEvictedKeys, err = s.meter.Int64Counter(redisEvictName,
		metric.WithDescription("Redis key evictions"))
	if err != nil {
		return err
	}

	redisClientsName := "redis_connected_clients"
	if s.telemCfg.MetricNames.Additional != nil {
		if n, ok := s.telemCfg.MetricNames.Additional[redisClientsName]; ok && n != "" {
			redisClientsName = n
		}
	}
	s.redisConnectedClients, err = s.meter.Int64UpDownCounter(redisClientsName,
		metric.WithDescription("Redis connected clients"))
	if err != nil {
		return err
	}

	// Replication offsets (master + slave)
	masterReplName := "redis_master_repl_offset"
	if s.telemCfg.MetricNames.Additional != nil {
		if n, ok := s.telemCfg.MetricNames.Additional[masterReplName]; ok && n != "" {
			masterReplName = n
		}
	}
	s.redisMasterReplOffset, err = s.meter.Int64UpDownCounter(masterReplName,
		metric.WithDescription("Redis master replication offset"))
	if err != nil {
		return err
	}

	slaveReplName := "redis_slave_repl_offset"
	if s.telemCfg.MetricNames.Additional != nil {
		if n, ok := s.telemCfg.MetricNames.Additional[slaveReplName]; ok && n != "" {
			slaveReplName = n
		}
	}
	s.redisSlaveReplOffset, err = s.meter.Int64UpDownCounter(slaveReplName,
		metric.WithDescription("Redis slave replication offset"))
	if err != nil {
		return err
	}

	// Kafka
	kafkaURPName := "kafka_controller_UnderReplicatedPartitions"
	if s.telemCfg.MetricNames.Additional != nil {
		if n, ok := s.telemCfg.MetricNames.Additional["kafka_controller_UnderReplicatedPartitions"]; ok && n != "" {
			kafkaURPName = n
		}
	}
	s.kafkaUnderReplicated, err = s.meter.Int64UpDownCounter(kafkaURPName,
		metric.WithDescription("Kafka under replicated partitions"))
	if err != nil {
		return err
	}

	kafkaReqName := "kafka_network_RequestMetrics_RequestsPerSec"
	if s.telemCfg.MetricNames.Additional != nil {
		if n, ok := s.telemCfg.MetricNames.Additional[kafkaReqName]; ok && n != "" {
			kafkaReqName = n
		}
	}
	s.kafkaRequestsPerSec, err = s.meter.Float64Counter(kafkaReqName,
		metric.WithDescription("Kafka network requests per second (incremental)"))
	if err != nil {
		return err
	}

	kafkaBytesName := "kafka_server_BrokerTopicMetrics_BytesInPerSec"
	if s.telemCfg.MetricNames.Additional != nil {
		if n, ok := s.telemCfg.MetricNames.Additional[kafkaBytesName]; ok && n != "" {
			kafkaBytesName = n
		}
	}
	s.kafkaBytesInPerSec, err = s.meter.Float64Counter(kafkaBytesName,
		metric.WithDescription("Kafka bytes in per second (incremental)"))
	if err != nil {
		return err
	}

	kafkaErrName := "kafka_network_RequestMetrics_ErrorsPerSec"
	if s.telemCfg.MetricNames.Additional != nil {
		if n, ok := s.telemCfg.MetricNames.Additional[kafkaErrName]; ok && n != "" {
			kafkaErrName = n
		}
	}
	s.kafkaRequestErrors, err = s.meter.Int64Counter(kafkaErrName,
		metric.WithDescription("Kafka request errors per second"))
	if err != nil {
		return err
	}

	kafkaISRName := "kafka_controller_IsrShrinksPerSec"
	if s.telemCfg.MetricNames.Additional != nil {
		if n, ok := s.telemCfg.MetricNames.Additional[kafkaISRName]; ok && n != "" {
			kafkaISRName = n
		}
	}
	s.kafkaISRChanges, err = s.meter.Float64Counter(kafkaISRName,
		metric.WithDescription("Kafka ISR changes per second (shrinks+expands)"))
	if err != nil {
		return err
	}

	// Node exporter
	nodeLoadName := "node_load1"
	if s.telemCfg.MetricNames.Additional != nil {
		if n, ok := s.telemCfg.MetricNames.Additional[nodeLoadName]; ok && n != "" {
			nodeLoadName = n
		}
	}
	s.nodeLoad1, err = s.meter.Float64UpDownCounter(nodeLoadName,
		metric.WithDescription("1-minute load average"))
	if err != nil {
		return err
	}

	nodeMemAvailName := "node_memory_MemAvailable_bytes"
	if s.telemCfg.MetricNames.Additional != nil {
		if n, ok := s.telemCfg.MetricNames.Additional[nodeMemAvailName]; ok && n != "" {
			nodeMemAvailName = n
		}
	}
	s.nodeMemoryAvailable, err = s.meter.Float64UpDownCounter(nodeMemAvailName,
		metric.WithDescription("Node available memory bytes"))
	if err != nil {
		return err
	}

	nodeMemTotalName := "node_memory_MemTotal_bytes"
	if s.telemCfg.MetricNames.Additional != nil {
		if n, ok := s.telemCfg.MetricNames.Additional[nodeMemTotalName]; ok && n != "" {
			nodeMemTotalName = n
		}
	}
	s.nodeMemoryTotal, err = s.meter.Float64UpDownCounter(nodeMemTotalName,
		metric.WithDescription("Node total memory bytes"))
	if err != nil {
		return err
	}

	nodeFsAvailName := "node_filesystem_avail_bytes"
	if s.telemCfg.MetricNames.Additional != nil {
		if n, ok := s.telemCfg.MetricNames.Additional[nodeFsAvailName]; ok && n != "" {
			nodeFsAvailName = n
		}
	}
	s.nodeFilesystemAvail, err = s.meter.Float64UpDownCounter(nodeFsAvailName,
		metric.WithDescription("Node filesystem available bytes"))
	if err != nil {
		return err
	}

	nodeFsSizeName := "node_filesystem_size_bytes"
	if s.telemCfg.MetricNames.Additional != nil {
		if n, ok := s.telemCfg.MetricNames.Additional[nodeFsSizeName]; ok && n != "" {
			nodeFsSizeName = n
		}
	}
	s.nodeFilesystemSize, err = s.meter.Float64UpDownCounter(nodeFsSizeName,
		metric.WithDescription("Node filesystem size bytes"))
	if err != nil {
		return err
	}

	nodeDiskIOName := "node_disk_io_time_seconds_total"
	if s.telemCfg.MetricNames.Additional != nil {
		if n, ok := s.telemCfg.MetricNames.Additional[nodeDiskIOName]; ok && n != "" {
			nodeDiskIOName = n
		}
	}
	s.nodeDiskIOTimeSeconds, err = s.meter.Float64Counter(nodeDiskIOName,
		metric.WithDescription("Node disk io time seconds (cumulative)"))

	// Node network metrics
	nodeNetLatencyName := "node_network_latency_ms"
	if n, ok := s.telemCfg.MetricNames.Additional[nodeNetLatencyName]; ok && n != "" {
		nodeNetLatencyName = n
	}
	s.nodeNetworkLatencyMs, err = s.meter.Float64UpDownCounter(nodeNetLatencyName,
		metric.WithDescription("Node network latency in milliseconds (gauge-like)"))
	if err != nil {
		return err
	}

	nodePacketDropsName := "node_network_packet_drops_total"
	if n, ok := s.telemCfg.MetricNames.Additional[nodePacketDropsName]; ok && n != "" {
		nodePacketDropsName = n
	}
	s.nodeNetworkPacketDrops, err = s.meter.Int64Counter(nodePacketDropsName,
		metric.WithDescription("Node network packet drops (incremental)"))
	if err != nil {
		return err
	}
	if err != nil {
		return err
	}

	nodeCtxName := "node_context_switches_total"
	if s.telemCfg.MetricNames.Additional != nil {
		if n, ok := s.telemCfg.MetricNames.Additional[nodeCtxName]; ok && n != "" {
			nodeCtxName = n
		}
	}
	s.nodeContextSwitches, err = s.meter.Float64Counter(nodeCtxName,
		metric.WithDescription("Node context switches (incremental)"))
	if err != nil {
		return err
	}

	return nil
}

// startBackgroundMetrics kicks off background goroutines that produce gauge-like
// values and counters independent of per-transaction events. It listens to the
// provided ctx and returns immediately if ctx is already cancelled.
func (s *Simulator) startBackgroundMetrics(ctx context.Context) {
	// Ensure RNG exists for background mutation/metrics
	if s.rng == nil {
		s.rng = rand.New(rand.NewSource(time.Now().UnixNano()))
	}
	// default interval if unset
	interval := s.dataInterval
	if interval <= 0 {
		interval = 5 * time.Second
	}

	// (scenario scheduler and histogram helpers are defined later)

	// initialize reasonable defaults
	s.stateLock.Lock()
	if s.stateJvmMax == 0 {
		s.stateJvmMax = 512 * 1024 * 1024 // 512MB
	}
	if s.stateJvmUsed == 0 {
		s.stateJvmUsed = float64(s.stateJvmMax) * 0.35
	}
	if s.stateTomcatMax == 0 {
		s.stateTomcatMax = 200
	}
	if s.stateTomcatBusy == 0 {
		s.stateTomcatBusy = 10
	}
	if s.stateCPU == 0 {
		s.stateCPU = 4.0
	}
	if s.stateProcessUptime == 0 {
		s.stateProcessUptime = 60 * 60
	}
	if s.stateFilesMax == 0 {
		s.stateFilesMax = 10240
	}
	if s.stateFilesOpen == 0 {
		s.stateFilesOpen = 120
	}
	if s.stateHikariMax == 0 {
		s.stateHikariMax = 100
	}
	if s.stateHikariActive == 0 {
		s.stateHikariActive = 5
	}
	if s.stateRedisMax == 0 {
		s.stateRedisMax = 256 * 1024 * 1024
	}
	if s.stateRedisUsed == 0 {
		s.stateRedisUsed = float64(s.stateRedisMax) * 0.2
	}
	if s.stateRedisMasterOffset == 0 {
		n := int64(10000)
		if n < 1 {
			n = 1
		}
		s.stateRedisMasterOffset = int64(1000 + s.rng.Int63n(n))
	}
	if s.stateRedisSlaveOffset == 0 {
		// slightly behind master
		n := int64(200)
		if n < 1 {
			n = 1
		}
		s.stateRedisSlaveOffset = s.stateRedisMasterOffset - int64(50+s.rng.Int63n(n))
		if s.stateRedisSlaveOffset < 0 {
			s.stateRedisSlaveOffset = 0
		}
	}
	if s.stateRedisClients == 0 {
		s.stateRedisClients = 10
	}
	if s.stateKafkaURP == 0 {
		s.stateKafkaURP = 0
	}
	if s.stateNodeMemTotal == 0 {
		s.stateNodeMemTotal = 4 * 1024 * 1024 * 1024 // 4GB
	}
	if s.stateNodeMemAvail == 0 {
		s.stateNodeMemAvail = s.stateNodeMemTotal * 0.6
	}
	if s.stateNodeLoad1 == 0 {
		s.stateNodeLoad1 = 0.4
	}
	if s.stateFsSize == 0 {
		s.stateFsSize = 120 * 1024 * 1024 * 1024 // 120GB
	}
	if s.stateFsAvail == 0 {
		s.stateFsAvail = s.stateFsSize * 0.7
	}
	if s.stateNodeNetworkLatency == 0 {
		s.stateNodeNetworkLatency = 3.0 // ms baseline
	}
	s.stateLock.Unlock()

	// initialize multipliers
	s.stateDbLatencyMult = 1.0
	s.stateGCPauseMult = 1.0
	s.stateFailureMult = 1.0
	s.stateTomcatLoadMult = 1.0
	s.stateRedisMemoryMult = 1.0
	s.stateKafkaURPMult = 1.0
	s.stateKafkaErrorMult = 1.0
	s.stateKeydbFailMult = 1.0
	s.stateNetworkLatencyMult = 1.0
	s.stateNetworkDropMult = 1.0
	// kafka throughput/requests multiplier (1.0 = normal, <1.0 reduced throughput)
	s.stateKafkaReqsMult = 1.0

	// replication offset multipliers
	s.stateRedisMasterReplMult = 1.0
	s.stateRedisSlaveReplMult = 1.0

	// background ticker
	ticker := time.NewTicker(interval)
	go func() {
		for {
			select {
			case <-ctx.Done():
				ticker.Stop()
				return
			case <-ticker.C:
				// reset multipliers to 1.0 then apply any active scenario effects
				if s.scenarioSched != nil {
					// default
					s.stateDbLatencyMult = 1.0
					s.stateGCPauseMult = 1.0
					s.stateFailureMult = 1.0
					s.stateTomcatLoadMult = 1.0
					s.stateRedisMemoryMult = 1.0
					s.stateKafkaURPMult = 1.0
					now := time.Now()
					active := s.scenarioSched.activeAt(now)
					for _, e := range active {
						for _, eff := range e.effects {
							fmt.Printf("[DEBUG] active effect: metric=%s op=%s value=%v\n", eff.Metric, eff.Op, eff.Value)
							switch eff.Metric {
							case "db_latency", "db_latency_seconds":
								switch eff.Op {
								case "scale":
									s.stateDbLatencyMult *= eff.Value
								case "add":
									s.stateDbLatencyMult += eff.Value
								case "set":
									s.stateDbLatencyMult = eff.Value
								case "ramp":
									s.stateDbLatencyMult += eff.Step
								}
							case "jvm_gc", "jvm_gc_pause_seconds":
								switch eff.Op {
								case "scale":
									s.stateGCPauseMult *= eff.Value
								case "add":
									s.stateGCPauseMult += eff.Value
								case "set":
									s.stateGCPauseMult = eff.Value
								case "ramp":
									s.stateGCPauseMult += eff.Step
								}
							case "transaction_failures", "transactions_failed_total":
								switch eff.Op {
								case "scale":
									s.stateFailureMult *= eff.Value
								case "add":
									s.stateFailureMult += eff.Value
								case "set":
									s.stateFailureMult = eff.Value
								case "ramp":
									s.stateFailureMult += eff.Step
								}
							case "tomcat_threads", "tomcat_threads_busy_threads":
								switch eff.Op {
								case "scale":
									s.stateTomcatLoadMult *= eff.Value
								case "add":
									s.stateTomcatLoadMult += eff.Value
								case "set":
									s.stateTomcatLoadMult = eff.Value
								case "ramp":
									s.stateTomcatLoadMult += eff.Step
								}
							case "redis_memory", "redis_memory_used_bytes":
								switch eff.Op {
								case "scale":
									s.stateRedisMemoryMult *= eff.Value
								case "add":
									s.stateRedisMemoryMult += eff.Value
								case "set":
									s.stateRedisMemoryMult = eff.Value
								case "ramp":
									s.stateRedisMemoryMult += eff.Step
								}
							case "redis_master_repl_offset":
								switch eff.Op {
								case "scale":
									s.stateRedisMasterReplMult *= eff.Value
								case "add":
									s.stateRedisMasterReplMult += eff.Value
								case "set":
									s.stateRedisMasterReplMult = eff.Value
								case "ramp":
									s.stateRedisMasterReplMult += eff.Step
								}
							case "redis_slave_repl_offset":
								switch eff.Op {
								case "scale":
									s.stateRedisSlaveReplMult *= eff.Value
								case "add":
									s.stateRedisSlaveReplMult += eff.Value
								case "set":
									s.stateRedisSlaveReplMult = eff.Value
								case "ramp":
									s.stateRedisSlaveReplMult += eff.Step
								}
							case "kafka_urp", "kafka_controller_UnderReplicatedPartitions":
								switch eff.Op {
								case "scale":
									s.stateKafkaURPMult *= eff.Value
								case "add":
									s.stateKafkaURPMult += eff.Value
								case "set":
									s.stateKafkaURPMult = eff.Value
								case "ramp":
									s.stateKafkaURPMult += eff.Step
								}
							case "kafka_error", "kafka_errors", "kafka_disk", "kafka_disk_failure", "kafka_disk_io":
								switch eff.Op {
								case "scale":
									s.stateKafkaErrorMult *= eff.Value
								case "add":
									s.stateKafkaErrorMult += eff.Value
								case "set":
									s.stateKafkaErrorMult = eff.Value
								case "ramp":
									s.stateKafkaErrorMult += eff.Step
								}
							case "kafka_requests", "kafka_throughput", "kafka_requests_per_sec":
								switch eff.Op {
								case "scale":
									s.stateKafkaReqsMult *= eff.Value
								case "add":
									s.stateKafkaReqsMult += eff.Value
								case "set":
									s.stateKafkaReqsMult = eff.Value
								case "ramp":
									s.stateKafkaReqsMult += eff.Step
								}
							case "keydb_memory_fault", "keydb_bad_memory", "valkey_bad_memory", "keydb_failure", "node_memory_corruption":
								switch eff.Op {
								case "scale":
									s.stateKeydbFailMult *= eff.Value
								case "add":
									s.stateKeydbFailMult += eff.Value
								case "set":
									s.stateKeydbFailMult = eff.Value
								case "ramp":
									s.stateKeydbFailMult += eff.Step
								}
							case "network_latency", "node_network_latency_ms", "network_latency_ms":
								switch eff.Op {
								case "scale":
									s.stateNetworkLatencyMult *= eff.Value
								case "add":
									s.stateNetworkLatencyMult += eff.Value
								case "set":
									s.stateNetworkLatencyMult = eff.Value
								case "ramp":
									s.stateNetworkLatencyMult += eff.Step
								}
							case "network_packet_drop", "node_network_packet_drops_total", "network_packet_drops", "packet_drop":
								switch eff.Op {
								case "scale":
									s.stateNetworkDropMult *= eff.Value
								case "add":
									s.stateNetworkDropMult += eff.Value
								case "set":
									s.stateNetworkDropMult = eff.Value
								case "ramp":
									s.stateNetworkDropMult += eff.Step
								}
							default:
								// unknown metric - ignore; we may wish to extend later
							}
						}
					}

				}

				// mutate state with small random walk, and record metrics as deltas
				// JVM memory used: random walk between 10%-95% of max
				{
					s.stateLock.Lock()
					// small noise
					delta := (s.rng.Float64() - 0.5) * 0.05 * float64(s.stateJvmMax)
					next := s.stateJvmUsed + delta
					if next < float64(s.stateJvmMax)*0.05 {
						next = float64(s.stateJvmMax) * 0.05
					}
					if next > float64(s.stateJvmMax)*0.98 {
						next = float64(s.stateJvmMax) * 0.98
					}
					add := next - s.stateJvmUsed
					if add != 0 {
						s.jvmMemoryUsed.Add(ctx, add)
					}
					s.stateJvmUsed = next
					s.jvmMemoryMax.Add(ctx, s.stateJvmMax-s.stateJvmMax) // ensure max exists (delta 0)
					s.stateLock.Unlock()
				}

				// GC pauses: small incremental counts and sums; occasionally a spike
				gcAdd := (0.001 + s.rng.Float64()*0.005) * s.stateGCPauseMult // seconds
				if s.rng.Float64() < 0.01 {                                   // rare long pause
					gcAdd += s.rng.Float64() * 0.5
				}
				s.safeAddFloat64Counter(s.jvmGCPauseSecondsSum, ctx, gcAdd)
				s.safeAddInt64Counter(s.jvmGCPauseSecondsCount, ctx, 1)

				// Tomcat thread pool
				{
					s.stateLock.Lock()
					n := 5
					if n <= 0 {
						n = 1
					}
					change := int64(s.rng.Intn(n) - 2)
					// scale tomcat thread busy changes by scenario multiplier
					change = int64(float64(change) * s.stateTomcatLoadMult)
					nextBusy := s.stateTomcatBusy + change
					if nextBusy < 0 {
						nextBusy = 0
					}
					if nextBusy > s.stateTomcatMax {
						nextBusy = s.stateTomcatMax
					}
					delta := nextBusy - s.stateTomcatBusy
					if delta != 0 {
						s.tomcatThreadsBusy.Add(ctx, delta)
					}
					s.stateTomcatBusy = nextBusy
					// queue grows when nearing max
					load := float64(s.stateTomcatBusy) / float64(s.stateTomcatMax)
					q := load * s.rng.Float64() * 2.0 * s.stateTomcatLoadMult
					qDelta := q - s.stateTomcatQueue
					if qDelta != 0 {
						s.tomcatThreadsQueueSec.Add(ctx, qDelta)
					}
					s.stateTomcatQueue = q
					s.stateLock.Unlock()
				}

				// process CPU usage small fluctuations
				{
					s.stateLock.Lock()
					cpuDelta := (s.rng.Float64() - 0.5) * 2.0
					nextCPU := s.stateCPU + cpuDelta
					if nextCPU < 0 {
						nextCPU = 0
					}
					if nextCPU > 100 {
						nextCPU = 100
					}
					if nextCPU != s.stateCPU {
						s.processCPUUsage.Add(ctx, nextCPU-s.stateCPU)
					}
					s.stateCPU = nextCPU
					s.stateLock.Unlock()
				}

				// Hikari connection pool usage
				{
					s.stateLock.Lock()
					n := 3
					if n <= 0 {
						n = 1
					}
					connDelta := int64(s.rng.Intn(n) - 1)
					nextConn := s.stateHikariActive + connDelta
					if nextConn < 0 {
						nextConn = 0
					}
					if nextConn > s.stateHikariMax {
						nextConn = s.stateHikariMax
					}
					if nextConn != s.stateHikariActive {
						s.hikariConnectionsActive.Add(ctx, nextConn-s.stateHikariActive)
					}
					s.stateHikariActive = nextConn
					s.stateLock.Unlock()
				}

				// Redis memory usage
				{
					s.stateLock.Lock()
					memDelta := (s.rng.Float64() - 0.5) * float64(5*1024*1024) * s.stateRedisMemoryMult
					next := s.stateRedisUsed + memDelta
					if next < 0 {
						next = 0
					}
					if next > s.stateRedisMax {
						next = s.stateRedisMax
						// start evictions when overflow
						s.safeAddInt64Counter(s.redisEvictedKeys, ctx, 1)
					}
					if next != s.stateRedisUsed {
						s.redisMemoryUsed.Add(ctx, next-s.stateRedisUsed)
					}
					s.stateRedisUsed = next
					s.stateLock.Unlock()
				}

				// Replication offsets: master increases gradually, slave trails behind
				{
					s.stateLock.Lock()
					// master progress (random-ish), scaled by scenario multiplier
					n := int64(500)
					if n < 1 {
						n = 1
					}
					masterInc := int64(10 + s.rng.Int63n(n))
					if s.stateRedisMasterReplMult != 1.0 {
						masterInc = int64(float64(masterInc) * s.stateRedisMasterReplMult)
					}
					nextMaster := s.stateRedisMasterOffset + masterInc
					if nextMaster < 0 {
						nextMaster = 0
					}
					if nextMaster != s.stateRedisMasterOffset {
						s.redisMasterReplOffset.Add(ctx, nextMaster-s.stateRedisMasterOffset)
					}
					s.stateRedisMasterOffset = nextMaster

					// slave tries to follow master but lags
					n = int64(200)
					if n < 1 {
						n = 1
					}
					lag := int64(1 + s.rng.Int63n(n))
					if s.stateRedisSlaveReplMult != 1.0 {
						lag = int64(float64(lag) * s.stateRedisSlaveReplMult)
					}
					nextSlave := s.stateRedisMasterOffset - lag
					if nextSlave < 0 {
						nextSlave = 0
					}
					if nextSlave != s.stateRedisSlaveOffset {
						s.redisSlaveReplOffset.Add(ctx, nextSlave-s.stateRedisSlaveOffset)
					}
					s.stateRedisSlaveOffset = nextSlave
					s.stateLock.Unlock()
				}

				// Kafka under-replicated partitions may happen rarely
				if s.rng.Float64() < 0.005*s.stateKafkaURPMult {
					n := int(5 * s.stateKafkaURPMult)
					if n < 1 {
						n = 1
					}
					delta := int64(float64(s.rng.Intn(n)+1) * s.stateKafkaURPMult)
					s.kafkaUnderReplicated.Add(ctx, delta)
					s.stateKafkaURP += delta
				}

				// Node exporter periodic counters
				{
					s.safeAddFloat64Counter(s.nodeDiskIOTimeSeconds, ctx, s.rng.Float64()*0.1)
					s.safeAddFloat64Counter(s.nodeContextSwitches, ctx, s.rng.Float64()*10)

					// small random walk for network latency (ms) scaled by scenario multiplier
					netDelta := (s.rng.Float64() - 0.5) * 2.0 * s.stateNetworkLatencyMult
					nextNet := s.stateNodeNetworkLatency + netDelta
					if nextNet < 0.2 {
						nextNet = 0.2
					}
					if nextNet > 2000 {
						nextNet = 2000
					}
					if nextNet != s.stateNodeNetworkLatency {
						s.nodeNetworkLatencyMs.Add(ctx, nextNet-s.stateNodeNetworkLatency)
					}
					s.stateNodeNetworkLatency = nextNet

					// packet drops: when scenario multiplier > 1, emit extra packet drops
					if s.stateNetworkDropMult > 1.0 {
						n := 1 + int(s.stateNetworkDropMult)
						if n < 1 {
							n = 1
						}
						addDrops := int64(s.rng.Intn(n))
						if addDrops > 0 {
							s.safeAddInt64Counter(s.nodeNetworkPacketDrops, ctx, addDrops)
							// also surface higher-level errors in messaging layers
							s.safeAddInt64Counter(s.kafkaRequestErrors, ctx, addDrops)
						}
					}
				}

				// Kafka request rate & bytes-in/second counters (incremental)
				// apply kafka requests multiplier to simulate throughput changes (mixed signals)
				reqVal := (50 + s.rng.Float64()*200) * s.stateKafkaReqsMult
				s.safeAddFloat64Counter(s.kafkaRequestsPerSec, ctx, reqVal)
				s.safeAddFloat64Counter(s.kafkaBytesInPerSec, ctx, 1024+s.rng.Float64()*10*1024)
				// when scenario indicates disk problems for kafka, emit additional request errors and ISR noise
				if s.stateKafkaErrorMult > 1.0 {
					// add variable number of request errors scaled by multiplier
					n := int64(1 + int64(s.stateKafkaErrorMult))
					if n < 1 {
						n = 1
					}
					addErrs := int64(1 + s.rng.Int63n(n))
					s.safeAddInt64Counter(s.kafkaRequestErrors, ctx, addErrs)
					// occasionally emit ISR changes (broker rebalances etc.)
					if s.rng.Float64() < 0.3*s.stateKafkaErrorMult {
						s.safeAddFloat64Counter(s.kafkaISRChanges, ctx, s.rng.Float64()*s.stateKafkaErrorMult)
					}
				}
			}
		}
	}()
}

// initLogger sets a logger for the simulator. Supported modes: "nop" (no-op), "stdout" (development stdout)
func (s *Simulator) initLogger(mode string) {
	switch mode {
	case "stdout":
		l, _ := zap.NewDevelopment()
		s.logger = l
	default:
		s.logger = zap.NewNop()
	}
}

func (s *Simulator) runSimulation(ctx context.Context) {
	// start background metrics generator
	go s.startBackgroundMetrics(ctx)
	// Calculate time distribution
	var timestamps []time.Time

	if s.timeWindowDuration > 0 {
		// Time series mode: distribute transactions over the time window
		startTime := time.Now().Add(s.startTimeOffset)

		// Calculate number of intervals
		numIntervals := int(s.timeWindowDuration / s.dataInterval)
		if numIntervals == 0 {
			numIntervals = 1
		}

		// Distribute transactions evenly across intervals
		txPerInterval := s.transactions / numIntervals
		remainder := s.transactions % numIntervals

		txIndex := 0
		for i := 0; i < numIntervals; i++ {
			intervalTime := startTime.Add(time.Duration(i) * s.dataInterval)
			txCount := txPerInterval
			if i < remainder {
				txCount++ // Distribute remainder across first intervals
			}

			// All transactions in this interval get the same timestamp
			for j := 0; j < txCount; j++ {
				timestamps = append(timestamps, intervalTime)
				txIndex++
			}
		}

		// Sort by timestamp to ensure chronological execution
		// Already sorted by construction, but explicit for clarity

		log.Printf("Generated %d transactions across %d intervals (%v each)",
			len(timestamps), numIntervals, s.dataInterval)

		// Execute transactions in chronological order, sleeping to match timestamps
		for i := 0; i < s.transactions; i++ {
			targetTime := timestamps[i]
			sleepDuration := time.Until(targetTime)

			if sleepDuration > 0 {
				log.Printf("Waiting %v until next interval...", sleepDuration)
				select {
				case <-time.After(sleepDuration):
				case <-ctx.Done():
					log.Println("Context cancelled; stopping scheduled simulation")
					return
				}
			}

			// Execute all transactions for this timestamp
			intervalStart := i
			intervalEnd := i + 1
			for intervalEnd < len(timestamps) && timestamps[intervalEnd].Equal(targetTime) {
				intervalEnd++
			}

			// Execute this batch concurrently
			var wg sync.WaitGroup
			sem := make(chan struct{}, s.concurrency)

			for j := intervalStart; j < intervalEnd; j++ {
				if ctx.Err() != nil {
					log.Println("Context cancelled; aborting batch execution")
					break
				}
				wg.Add(1)
				go func(txID int) {
					defer wg.Done()
					sem <- struct{}{}
					defer func() { <-sem }()

					s.simulateTransaction(ctx, fmt.Sprintf("tx-%06d", txID))
				}(j)
			}

			wg.Wait()

			// Skip to next interval
			i = intervalEnd - 1
		}
	} else {
		// Instant mode: all transactions at current time
		var wg sync.WaitGroup
		sem := make(chan struct{}, s.concurrency)

		for i := 0; i < s.transactions; i++ {
			if ctx.Err() != nil {
				log.Println("Context cancelled; aborting instant simulation")
				break
			}
			wg.Add(1)
			go func(txID int) {
				defer wg.Done()
				sem <- struct{}{}
				defer func() { <-sem }()

				s.simulateTransaction(ctx, fmt.Sprintf("tx-%06d", txID))
			}(i)
		}

		wg.Wait()
	}
}

func (s *Simulator) simulateTransaction(ctx context.Context, transactionID string) {
	// Ensure RNG is initialized when tests or callers create a zero-value Simulator
	if s.rng == nil {
		s.rng = rand.New(rand.NewSource(time.Now().UnixNano()))
	}
	start := time.Now()

	// Generate transaction details
	n := s.maxAmountPaise - s.minAmountPaise + 1
	if n < 1 {
		n = 1
	}
	amountPaise := s.minAmountPaise + s.rng.Int63n(n)
	customerID := fmt.Sprintf("cust-%04d", s.rng.Intn(10000))
	channel := []string{"mobile", "web", "api"}[s.rng.Intn(3)]

	// labels: OrgId/OrgName/transaction_type (configurable)
	var orgId, orgName, txType string
	if len(s.telemCfg.Labels.OrgIds) > 0 {
		orgId = s.telemCfg.Labels.OrgIds[s.rng.Intn(len(s.telemCfg.Labels.OrgIds))]
	}
	if len(s.telemCfg.Labels.OrgNames) > 0 {
		orgName = s.telemCfg.Labels.OrgNames[s.rng.Intn(len(s.telemCfg.Labels.OrgNames))]
	}
	if len(s.telemCfg.Labels.TransactionTypes) > 0 {
		txType = s.telemCfg.Labels.TransactionTypes[s.rng.Intn(len(s.telemCfg.Labels.TransactionTypes))]
	}

	// Determine if this transaction should fail
	var shouldFail bool
	if s.failureSched != nil {
		shouldFail = s.failureSched.ShouldFail(time.Now())
	} else {
		// apply scenario-driven multiplier to failure rate
		effRate := s.failureRate * s.stateFailureMult
		shouldFail = s.rng.Float64() < effRate
	}
	var failureComponent string
	if shouldFail {
		switch s.failureMode {
		case FailureModeKafka:
			failureComponent = "kafka"
		case FailureModeCassandra:
			failureComponent = "cassandra"
		case FailureModeKeyDB:
			failureComponent = "keydb"
		case FailureModeAPIGateway:
			failureComponent = "api-gateway"
		case FailureModeTPS:
			failureComponent = "tps"
		case FailureModeMixed:
			components := []string{"kafka", "cassandra", "keydb", "api-gateway", "tps"}
			failureComponent = components[s.rng.Intn(len(components))]
		}
	}

	// Start root span for API Gateway
	apiGatewayName := "api-gateway"
	if s.telemCfg.ServiceNames.APIGateway != "" {
		apiGatewayName = s.telemCfg.ServiceNames.APIGateway
	}

	ctx, rootSpan := s.tracer.Start(ctx, apiGatewayName+".receive_transaction",
		trace.WithSpanKind(trace.SpanKindServer),
		trace.WithAttributes(
			attribute.String("transaction_id", transactionID),
			attribute.String("customer_id", customerID),
			attribute.String("channel", channel),
			attribute.Int64("amount_paisa", amountPaise),
			attribute.String("currency", currencyINR),
		))
	defer rootSpan.End()

	// Log API Gateway request received
	s.logger.Info("Transaction request received",
		zap.String("transaction_id", transactionID),
		zap.String("service_name", apiGatewayName),
		zap.Int64("amount_paisa", amountPaise),
		zap.String("currency", currencyINR),
		zap.String("severity", "INFO"),
	)

	// Simulate API Gateway auth check
	if failureComponent == "api-gateway" && shouldFail {
		rootSpan.SetStatus(codes.Error, "Auth validation failed")
		rootSpan.SetAttributes(
			attribute.String("final_status", "error"),
			attribute.String("error_type", "auth_failure"),
		)
		s.safeAddInt64Counter(s.transactionsFailedTotal, ctx, 1, attribute.String("service_name", apiGatewayName))
		s.logger.Error("Auth validation failed",
			zap.String("transaction_id", transactionID),
			zap.String("service_name", apiGatewayName),
			zap.Int64("amount_paisa", amountPaise),
			zap.String("currency", currencyINR),
			zap.String("severity", "ERROR"),
			zap.String("failure_reason", "auth_failure"),
		)
		return
	}

	// Call TPS
	tpsName := "tps"
	if s.telemCfg.ServiceNames.TPS != "" {
		tpsName = s.telemCfg.ServiceNames.TPS
	}

	ctx, tpsClientSpan := s.tracer.Start(ctx, apiGatewayName+".call_tps",
		trace.WithSpanKind(trace.SpanKindClient),
		trace.WithAttributes(
			attribute.String("transaction_id", transactionID),
			attribute.String("service_name", apiGatewayName),
			attribute.String("peer.service", tpsName),
		))
	result := s.simulateTPS(ctx, transactionID, amountPaise, customerID, failureComponent, shouldFail)
	tpsClientSpan.End()

	// API Gateway consumes from Kafka and writes to Cassandra
	if result.FinalStatus == "success" {
		s.simulateAPIGatewayKafkaConsumer(ctx, transactionID, amountPaise, failureComponent == "kafka" && shouldFail)
	}

	// Update root span
	rootSpan.SetAttributes(
		attribute.String("final_status", result.FinalStatus),
	)
	if result.FinalStatus == "error" {
		rootSpan.SetStatus(codes.Error, result.ErrorReason)
	}

	// Record final metrics
	attrs := []attribute.KeyValue{attribute.String("service_name", apiGatewayName)}
	if orgId != "" {
		attrs = append(attrs, attribute.String("OrgId", orgId))
	}
	if orgName != "" {
		attrs = append(attrs, attribute.String("OrgName", orgName))
	}
	if txType != "" {
		attrs = append(attrs, attribute.String("transaction_type", txType))
	}

	s.safeAddInt64Counter(s.transactionsTotal, ctx, 1, attrs...)
	s.safeAddInt64Counter(s.transactionAmountSum, ctx, result.AmountPaise, append(attrs, attribute.String("currency", currencyINR))...)
	s.safeAddInt64Counter(s.transactionAmountCount, ctx, 1, append(attrs, attribute.String("currency", currencyINR))...)

	if result.FinalStatus == "error" {
		s.safeAddInt64Counter(s.transactionsFailedTotal, ctx, 1, attrs...)
	}

	duration := time.Since(start).Seconds() * s.stateDbLatencyMult
	// record both OTLP histogram and Prom-style bucket counters
	s.transactionLatency.Record(ctx, duration, metric.WithAttributes(attrs...))
	s.recordTransactionLatencyBuckets(ctx, duration, attrs...)
}

func (s *Simulator) simulateTPS(ctx context.Context, transactionID string, amountPaise int64, customerID string, failureComponent string, shouldFail bool) TransactionResult {
	if s.rng == nil {
		s.rng = rand.New(rand.NewSource(time.Now().UnixNano()))
	}
	tpsName := "tps"
	if s.telemCfg.ServiceNames.TPS != "" {
		tpsName = s.telemCfg.ServiceNames.TPS
	}

	ctx, span := s.tracer.Start(ctx, tpsName+".process_transaction",
		trace.WithSpanKind(trace.SpanKindServer),
		trace.WithAttributes(
			attribute.String("transaction_id", transactionID),
			attribute.String("service_name", tpsName),
		))
	defer span.End()

	result := TransactionResult{
		TransactionID: transactionID,
		AmountPaise:   amountPaise,
		CustomerID:    customerID,
		FinalStatus:   "success",
	}

	// Simulate 22 TPS steps
	for step := 1; step <= 22; step++ {
		stepCtx, stepSpan := s.tracer.Start(ctx, fmt.Sprintf(tpsName+".step_%02d", step),
			trace.WithSpanKind(trace.SpanKindInternal),
			trace.WithAttributes(
				attribute.String("transaction_id", transactionID),
				attribute.String("service_name", tpsName),
				attribute.Int("step_number", step),
			))

		// Simulate different operations based on step
		switch {
		case step <= 5:
			// Auth and validation steps - use Cassandra
			if err := s.simulateCassandraOp(stepCtx, transactionID, "auth_lookup", failureComponent == "cassandra" && shouldFail); err != nil {
				result.FinalStatus = "error"
				result.ErrorReason = "cassandra_auth_failure"
				stepSpan.SetStatus(codes.Error, err.Error())
				s.logger.Error("TPS step failed",
					zap.String("transaction_id", transactionID),
					zap.String("service_name", tpsName),
					zap.Int("step_number", step),
					zap.Int64("amount_paisa", amountPaise),
					zap.String("currency", currencyINR),
					zap.String("severity", "ERROR"),
					zap.String("failure_reason", "cassandra_auth_failure"),
				)
				stepSpan.End()
				return result
			}
		case step <= 15:
			// Processing steps - use KeyDB for state
			if err := s.simulateKeyDBOp(stepCtx, transactionID, fmt.Sprintf("state_update_%d", step), failureComponent == "keydb" && shouldFail); err != nil {
				result.FinalStatus = "error"
				result.ErrorReason = "keydb_state_failure"
				stepSpan.SetStatus(codes.Error, err.Error())
				s.logger.Error("TPS step failed",
					zap.String("transaction_id", transactionID),
					zap.String("service_name", tpsName),
					zap.Int("step_number", step),
					zap.Int64("amount_paisa", amountPaise),
					zap.String("currency", currencyINR),
					zap.String("severity", "ERROR"),
					zap.String("failure_reason", "keydb_state_failure"),
				)
				stepSpan.End()
				return result
			}
		case step == 16:
			// Final validation - use Cassandra
			if err := s.simulateCassandraOp(stepCtx, transactionID, "final_validation", failureComponent == "cassandra" && shouldFail); err != nil {
				result.FinalStatus = "error"
				result.ErrorReason = "cassandra_validation_failure"
				stepSpan.SetStatus(codes.Error, err.Error())
				stepSpan.End()
				return result
			}
		case step >= 17 && step <= 20:
			// Internal processing
			time.Sleep(time.Duration(10+s.rng.Intn(20)) * time.Millisecond)
		case step == 21:
			// Produce to Kafka
			if err := s.simulateKafkaProduce(stepCtx, transactionID, amountPaise, failureComponent == "kafka" && shouldFail); err != nil {
				result.FinalStatus = "error"
				result.ErrorReason = "kafka_produce_failure"
				stepSpan.SetStatus(codes.Error, err.Error())
				s.logger.Error("TPS step failed",
					zap.String("transaction_id", transactionID),
					zap.String("service_name", tpsName),
					zap.Int("step_number", step),
					zap.Int64("amount_paisa", amountPaise),
					zap.String("currency", currencyINR),
					zap.String("severity", "ERROR"),
					zap.String("failure_reason", "kafka_produce_failure"),
				)
				stepSpan.End()
				return result
			}
		case step == 22:
			// Final commit
			time.Sleep(time.Duration(5+s.rng.Intn(10)) * time.Millisecond)
		}

		stepSpan.End()
	}

	return result
}

func (s *Simulator) simulateCassandraOp(ctx context.Context, transactionID, operation string, shouldFail bool) error {
	if s.rng == nil {
		s.rng = rand.New(rand.NewSource(time.Now().UnixNano()))
	}
	cassName := "cassandra-client"
	if s.telemCfg.ServiceNames.CassandraClient != "" {
		cassName = s.telemCfg.ServiceNames.CassandraClient
	}

	ctx, span := s.tracer.Start(ctx, "cassandra."+operation,
		trace.WithSpanKind(trace.SpanKindClient),
		trace.WithAttributes(
			attribute.String("transaction_id", transactionID),
			attribute.String("service_name", cassName),
			attribute.String("db.system", "cassandra"),
			attribute.String("db.operation", operation),
		))
	defer span.End()

	start := time.Now()

	if shouldFail {
		span.SetStatus(codes.Error, "Cassandra operation failed")
		s.logger.Error("Cassandra operation failed",
			zap.String("transaction_id", transactionID),
			zap.String("service_name", cassName),
			zap.String("severity", "ERROR"),
			zap.String("failure_reason", "cassandra_timeout"),
		)
		return fmt.Errorf("cassandra operation failed")
	}

	// Simulate latency
	time.Sleep(time.Duration(20+s.rng.Intn(30)) * time.Millisecond)
	duration := time.Since(start).Seconds() * s.stateDbLatencyMult

	s.safeAddInt64Counter(s.dbOpsTotal, ctx, 1, attribute.String("db_system", "cassandra"))
	s.dbLatency.Record(ctx, duration, metric.WithAttributes(attribute.String("db_system", "cassandra")))
	s.recordDBLatencyBuckets(ctx, duration, attribute.String("db_system", "cassandra"))

	return nil
}

func (s *Simulator) simulateKeyDBOp(ctx context.Context, transactionID, operation string, shouldFail bool) error {
	if s.rng == nil {
		s.rng = rand.New(rand.NewSource(time.Now().UnixNano()))
	}
	keyName := "keydb-client"
	if s.telemCfg.ServiceNames.KeyDBClient != "" {
		keyName = s.telemCfg.ServiceNames.KeyDBClient
	}

	ctx, span := s.tracer.Start(ctx, "keydb."+operation,
		trace.WithSpanKind(trace.SpanKindClient),
		trace.WithAttributes(
			attribute.String("transaction_id", transactionID),
			attribute.String("service_name", keyName),
			attribute.String("db.system", "valkey"),
			attribute.String("db.operation", operation),
		))
	defer span.End()

	start := time.Now()

	// consider scenario-driven KeyDB failures (e.g., bad memory modules / corrupted memory)
	combinedFail := shouldFail
	if s.stateKeydbFailMult > 1.0 && s.rng.Float64() < ((s.stateKeydbFailMult-1.0)*0.25) {
		combinedFail = true
	}

	if combinedFail {
		span.SetStatus(codes.Error, "KeyDB operation failed")
		// make log reason more descriptive when scenario multiplier drove it
		reason := "keydb_connection_error"
		if s.stateKeydbFailMult > 1.0 {
			reason = "keydb_memory_corruption"
			// also emit some redis/keydb related noise so metrics reflect memory fault
			s.safeAddInt64Counter(s.redisKeyspaceMisses, ctx, 1)
			s.safeAddInt64Counter(s.redisEvictedKeys, ctx, 1)
		}
		s.logger.Error("KeyDB operation failed",
			zap.String("transaction_id", transactionID),
			zap.String("service_name", keyName),
			zap.String("severity", "ERROR"),
			zap.String("failure_reason", reason),
		)
		return fmt.Errorf("keydb operation failed")
	}

	// Simulate latency
	time.Sleep(time.Duration(5+s.rng.Intn(15)) * time.Millisecond)
	duration := time.Since(start).Seconds() * s.stateDbLatencyMult

	s.safeAddInt64Counter(s.dbOpsTotal, ctx, 1, attribute.String("db_system", "valkey"))
	s.dbLatency.Record(ctx, duration, metric.WithAttributes(attribute.String("db_system", "valkey")))
	s.recordDBLatencyBuckets(ctx, duration, attribute.String("db_system", "valkey"))

	return nil
}

func (s *Simulator) simulateKafkaProduce(ctx context.Context, transactionID string, amountPaise int64, shouldFail bool) error {
	if s.rng == nil {
		s.rng = rand.New(rand.NewSource(time.Now().UnixNano()))
	}
	kpName := "kafka-producer"
	if s.telemCfg.ServiceNames.KafkaProducer != "" {
		kpName = s.telemCfg.ServiceNames.KafkaProducer
	}

	ctx, span := s.tracer.Start(ctx, "kafka.produce",
		trace.WithSpanKind(trace.SpanKindProducer),
		trace.WithAttributes(
			attribute.String("transaction_id", transactionID),
			attribute.String("service_name", kpName),
			attribute.String("messaging.system", "kafka"),
			attribute.String("messaging.operation", "produce"),
			attribute.String("messaging.destination", "transaction-log"),
		))
	defer span.End()

	start := time.Now()

	// consider scenario-driven Kafka errors (e.g., disk failure causing produce errors)
	combinedFail := shouldFail
	if s.stateKafkaErrorMult > 1.0 && s.rng.Float64() < ((s.stateKafkaErrorMult-1.0)*0.25) {
		combinedFail = true
	}

	// consider network issues: increased latency and packet drops
	// apply latency multiplier to produce latency
	if s.stateNetworkLatencyMult > 1.0 {
		// scale the local latency simulated for produce
		// if sleep uses stateDbLatencyMult elsewhere we'll multiply the produce wait
		// add a small network-induced delay
		extra := float64(5+s.rng.Intn(20)) * s.stateNetworkLatencyMult
		time.Sleep(time.Duration(extra) * time.Millisecond)
	}
	if s.stateNetworkDropMult > 1.0 && s.rng.Float64() < (0.01*s.stateNetworkDropMult) {
		combinedFail = true
	}

	if combinedFail {
		span.SetStatus(codes.Error, "Kafka produce failed")
		// increment error counter when scenario is causing errors
		s.safeAddInt64Counter(s.kafkaRequestErrors, ctx, 1)
		return fmt.Errorf("kafka produce failed")
	}

	// Simulate latency
	time.Sleep(time.Duration(10+s.rng.Intn(20)) * time.Millisecond)
	duration := time.Since(start).Seconds() * s.stateDbLatencyMult

	s.safeAddInt64Counter(s.kafkaProduceTotal, ctx, 1, attribute.String("service_name", kpName), attribute.String("messaging.destination", "transaction-log"))
	s.kafkaProduceLatency.Record(ctx, duration,
		metric.WithAttributes(
			attribute.String("service_name", kpName),
			attribute.String("messaging.destination", "transaction-log"),
		))

	return nil
}

func (s *Simulator) simulateKafkaConsumer(ctx context.Context, transactionID string, amountPaise int64, shouldFail bool) {
	if s.rng == nil {
		s.rng = rand.New(rand.NewSource(time.Now().UnixNano()))
	}
	kcName := "kafka-consumer"
	if s.telemCfg.ServiceNames.KafkaConsumer != "" {
		kcName = s.telemCfg.ServiceNames.KafkaConsumer
	}

	ctx, span := s.tracer.Start(ctx, "kafka.consume",
		trace.WithSpanKind(trace.SpanKindConsumer),
		trace.WithAttributes(
			attribute.String("transaction_id", transactionID),
			attribute.String("service_name", kcName),
			attribute.String("messaging.system", "kafka"),
			attribute.String("messaging.operation", "consume"),
			attribute.String("messaging.destination", "transaction-log"),
		))
	defer span.End()

	start := time.Now()

	// consider scenario-driven Kafka consume errors and network issues
	combinedFail := shouldFail
	if s.stateKafkaErrorMult > 1.0 && s.rng.Float64() < ((s.stateKafkaErrorMult-1.0)*0.25) {
		combinedFail = true
	}

	// network-induced latency
	if s.stateNetworkLatencyMult > 1.0 {
		extra := float64(10+s.rng.Intn(50)) * s.stateNetworkLatencyMult
		time.Sleep(time.Duration(extra) * time.Millisecond)
	}

	// packet drops may cause consume to fail or be noisy
	if s.stateNetworkDropMult > 1.0 && s.rng.Float64() < (0.02*s.stateNetworkDropMult) {
		combinedFail = true
	}

	if combinedFail {
		span.SetStatus(codes.Error, "Kafka consume failed")
		s.safeAddInt64Counter(s.kafkaRequestErrors, ctx, 1)
		return
	}

	// Simulate processing delay
	baseDelayMs := float64(50 + s.rng.Intn(100))
	// add network-induced latency when present
	if s.stateNetworkLatencyMult > 1.0 {
		baseDelayMs = baseDelayMs * s.stateNetworkLatencyMult
	}
	time.Sleep(time.Duration(baseDelayMs) * time.Millisecond)

	// Write final state to Cassandra
	if err := s.simulateCassandraOp(ctx, transactionID, "final_state_write", false); err != nil {
		span.SetStatus(codes.Error, "Final state write failed")
		return
	}

	duration := time.Since(start).Seconds()
	s.safeAddInt64Counter(s.kafkaConsumeTotal, ctx, 1, attribute.String("service_name", kcName), attribute.String("messaging.destination", "transaction-log"))
	s.kafkaConsumeLatency.Record(ctx, duration,
		metric.WithAttributes(
			attribute.String("service_name", kcName),
			attribute.String("messaging.destination", "transaction-log"),
		))
}

func (s *Simulator) simulateAPIGatewayKafkaConsumer(ctx context.Context, transactionID string, amountPaise int64, shouldFail bool) {
	if s.rng == nil {
		s.rng = rand.New(rand.NewSource(time.Now().UnixNano()))
	}
	// API Gateway consumes from Kafka
	agName := "api-gateway"
	if s.telemCfg.ServiceNames.APIGateway != "" {
		agName = s.telemCfg.ServiceNames.APIGateway
	}

	ctx, consumerSpan := s.tracer.Start(ctx, agName+".kafka_consume",
		trace.WithSpanKind(trace.SpanKindConsumer),
		trace.WithAttributes(
			attribute.String("transaction_id", transactionID),
			attribute.String("service_name", agName),
			attribute.String("messaging.system", "kafka"),
			attribute.String("messaging.operation", "consume"),
			attribute.String("messaging.destination", "transaction-log"),
		))
	defer consumerSpan.End()

	start := time.Now()

	// also consider failures due to packet drop multiplier
	consumerFail := shouldFail
	if s.stateNetworkDropMult > 1.0 && s.rng.Float64() < (0.02*s.stateNetworkDropMult) {
		consumerFail = true
	}
	if consumerFail {
		consumerSpan.SetStatus(codes.Error, "Kafka consume failed")
		return
	}

	// Simulate processing delay
	time.Sleep(time.Duration(50+s.rng.Intn(100)) * time.Millisecond)

	// API Gateway writes final state to Cassandra
	if err := s.simulateAPIGatewayCassandraWrite(ctx, transactionID, "final_state_write", false); err != nil {
		consumerSpan.SetStatus(codes.Error, "Final state write failed")
		return
	}

	duration := time.Since(start).Seconds()
	s.safeAddInt64Counter(s.kafkaConsumeTotal, ctx, 1, attribute.String("service_name", agName), attribute.String("messaging.destination", "transaction-log"))
	s.kafkaConsumeLatency.Record(ctx, duration,
		metric.WithAttributes(
			attribute.String("service_name", agName),
			attribute.String("messaging.destination", "transaction-log"),
		))
}

func (s *Simulator) simulateAPIGatewayCassandraWrite(ctx context.Context, transactionID, operation string, shouldFail bool) error {
	if s.rng == nil {
		s.rng = rand.New(rand.NewSource(time.Now().UnixNano()))
	}
	agName := "api-gateway"
	if s.telemCfg.ServiceNames.APIGateway != "" {
		agName = s.telemCfg.ServiceNames.APIGateway
	}

	ctx, span := s.tracer.Start(ctx, agName+".cassandra_write",
		trace.WithSpanKind(trace.SpanKindClient),
		trace.WithAttributes(
			attribute.String("transaction_id", transactionID),
			attribute.String("service_name", agName),
			attribute.String("db.system", "cassandra"),
			attribute.String("db.operation", operation),
		))
	defer span.End()

	start := time.Now()

	if shouldFail {
		span.SetStatus(codes.Error, "Cassandra write failed")
		return fmt.Errorf("cassandra write failed")
	}

	// Simulate latency
	time.Sleep(time.Duration(20+s.rng.Intn(30)) * time.Millisecond)
	duration := time.Since(start).Seconds()

	s.safeAddInt64Counter(s.dbOpsTotal, ctx, 1, attribute.String("db_system", "cassandra"))
	s.dbLatency.Record(ctx, duration, metric.WithAttributes(attribute.String("db_system", "cassandra")))
	s.recordDBLatencyBuckets(ctx, duration, attribute.String("db_system", "cassandra"))

	return nil
}

// recordTransactionLatencyBuckets updates Prometheus-style cumulative bucket counters
// and the associated _sum and _count for transaction latency. attrs should include
// any attributes (e.g., service_name, OrgId) to be attached to the metric.
func (s *Simulator) recordTransactionLatencyBuckets(ctx context.Context, v float64, attrs ...attribute.KeyValue) {
	// ensure underlying histogram recorded (OTel) was already called; update explicit counters for PromQL
	for _, b := range s.transactionLatencyBuckets {
		if v <= b {
			// le label as string
			le := fmt.Sprintf("%g", b)
			// combine attrs with le attribute
			combined := append(attrs, attribute.String("le", le))
			s.safeAddFloat64Counter(s.transactionLatencyBucket, ctx, 1.0, combined...)
		}
	}
	// +Inf bucket always receives the count
	combinedInf := append(attrs, attribute.String("le", "+Inf"))
	s.safeAddFloat64Counter(s.transactionLatencyBucket, ctx, 1.0, combinedInf...)

	// sum and count
	s.safeAddFloat64Counter(s.transactionLatencySum, ctx, v, attrs...)
	s.safeAddInt64Counter(s.transactionLatencyCount, ctx, 1, attrs...)
}

// recordDBLatencyBuckets updates DB histogram counters similarly
func (s *Simulator) recordDBLatencyBuckets(ctx context.Context, v float64, attrs ...attribute.KeyValue) {
	for _, b := range s.dbLatencyBuckets {
		if v <= b {
			le := fmt.Sprintf("%g", b)
			combined := append(attrs, attribute.String("le", le))
			s.safeAddFloat64Counter(s.dbLatencyBucket, ctx, 1.0, combined...)
		}
	}
	combinedInf := append(attrs, attribute.String("le", "+Inf"))
	s.safeAddFloat64Counter(s.dbLatencyBucket, ctx, 1.0, combinedInf...)

	s.safeAddFloat64Counter(s.dbLatencySum, ctx, v, attrs...)
	s.safeAddInt64Counter(s.dbLatencyCount, ctx, 1, attrs...)
}

// safeAdd helpers ensure we never add negative values to cumulative counters.
// Counters must only be incremented with non-negative values; helpers also
// accept optional attributes to keep call sites concise.
func (s *Simulator) safeAddInt64Counter(c metric.Int64Counter, ctx context.Context, v int64, attrs ...attribute.KeyValue) {
	if v < 0 {
		v = 0
	}
	if len(attrs) > 0 {
		c.Add(ctx, v, metric.WithAttributes(attrs...))
	} else {
		c.Add(ctx, v)
	}
}

func (s *Simulator) safeAddFloat64Counter(c metric.Float64Counter, ctx context.Context, v float64, attrs ...attribute.KeyValue) {
	if v < 0 {
		v = 0
	}
	if len(attrs) > 0 {
		c.Add(ctx, v, metric.WithAttributes(attrs...))
	} else {
		c.Add(ctx, v)
	}
}

// scenarioScheduler implements runtime schedule for configured scenarios.
type scenarioScheduler struct {
	entries []struct {
		start   time.Time
		end     time.Time
		labels  map[string][]string
		effects []Effect
		name    string
	}
}

// newScenarioScheduler builds scheduler entries from failure config scenarios.
func newScenarioScheduler(cfg FailureConfig, simStart time.Time) (*scenarioScheduler, error) {
	ss := &scenarioScheduler{}
	for _, sc := range cfg.Scenarios {
		var s time.Time
		if d, err := time.ParseDuration(sc.Start); err == nil {
			s = simStart.Add(d)
		} else if t, err := time.Parse(time.RFC3339, sc.Start); err == nil {
			s = t
		} else {
			return nil, fmt.Errorf("invalid scenario start: %s", sc.Start)
		}

		dur, err := time.ParseDuration(sc.Duration)
		if err != nil {
			return nil, fmt.Errorf("invalid scenario duration: %s", sc.Duration)
		}

		ss.entries = append(ss.entries, struct {
			start   time.Time
			end     time.Time
			labels  map[string][]string
			effects []Effect
			name    string
		}{start: s, end: s.Add(dur), labels: sc.Labels, effects: sc.Effects, name: sc.Name})
	}

	return ss, nil
}

// activeAt returns the active entries at given time.
func (ss *scenarioScheduler) activeAt(at time.Time) []struct {
	start   time.Time
	end     time.Time
	labels  map[string][]string
	effects []Effect
	name    string
} {
	var out []struct {
		start   time.Time
		end     time.Time
		labels  map[string][]string
		effects []Effect
		name    string
	}
	for _, e := range ss.entries {
		if !at.Before(e.start) && at.Before(e.end) {
			out = append(out, e)
		}
	}
	return out
}
