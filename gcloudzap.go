/*
Package gcloudzap provides a zap logger that forwards entries to the Google
Stackdriver Logging service as structured payloads.

All zap.Logger instances created with this package are safe for concurrent
use.

Writes are buffered and asynchronous. These benchmarks, on a MacBook Pro 2.4
GHz Core i5, are a loose approximation of latencies on the critical path for
the zapcore.Core implementation provided by this package.

	$ go test -bench . github.com/jonstaryuk/gcloudzap
	goos: darwin
	goarch: amd64
	pkg: github.com/jonstaryuk/gcloudzap
	BenchmarkCoreClone-4   	 2000000	       607 ns/op
	BenchmarkCoreWrite-4   	 1000000	      2811 ns/op


Zap docs: https://godoc.org/go.uber.org/zap

Stackdriver Logging docs: https://cloud.google.com/logging/docs/

*/
package gcloudzap // import "github.com/jonstaryuk/gcloudzap"

import (
	"context"
	"fmt"

	gcl "cloud.google.com/go/logging"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

func newClient(projectID string) (*gcl.Client, error) {
	if projectID == "" {
		return nil, newError("the provided projectID is empty")
	}

	return gcl.NewClient(context.Background(), projectID)
}

// NewDevelopment builds a development Logger that writes DebugLevel and above
// logs to standard error in a human-friendly format, as well as to
// Stackdriver using Application Default Credentials.
func NewDevelopment(projectID string, logID string) (*zap.Logger, error) {
	if logID == "" {
		return nil, fmt.Errorf("the provided logID is empty")
	}

	client, err := newClient(projectID)
	if err != nil {
		return nil, newError("creating Google Logging client: %v", err)
	}

	return New(zap.NewDevelopmentConfig(), client, logID)
}

// NewProduction builds a production Logger that writes InfoLevel and above
// logs to standard error as JSON, as well as to Stackdriver using Application
// Default Credentials.
func NewProduction(projectID string, logID string) (*zap.Logger, error) {
	if logID == "" {
		return nil, fmt.Errorf("the provided logID is empty")
	}

	client, err := newClient(projectID)
	if err != nil {
		return nil, newError("creating Google Logging client: %v", err)
	}

	return New(zap.NewProductionConfig(), client, logID)
}

// New creates a new zap.Logger which will write entries to Stackdriver in
// addition to the destination specified by the provided zap configuration.
func New(cfg zap.Config, client *gcl.Client, logID string, opts ...zap.Option) (*zap.Logger, error) {
	zl, err := cfg.Build(opts...)
	if err != nil {
		return nil, err
	}

	if client == nil {
		return nil, fmt.Errorf("The provided GCL client is nil")
	}

	tee := Tee(zl.Core(), client, logID)
	return zap.New(tee, opts...), nil
}

// A Core implements zapcore.Core and writes entries to a Logger from the
// Google Cloud package.
//
// It's safe for concurrent use by multiple goroutines as long as it's not
// mutated after first use.
type Core struct {
	// Logger is a logging.Logger instance from the Google Cloud Platform Go
	// library.
	Logger GoogleCloudLogger

	// Provide your own mapping of zapcore's Levels to Google's Severities, or
	// use DefaultSeverityMapping. All of the Core's children will default to
	// using this map.
	//
	// This must not be mutated after the Core's first use.
	SeverityMapping map[zapcore.Level]gcl.Severity

	// MinLevel is the minimum level for a log entry to be written.
	MinLevel zapcore.Level

	// fields should be built once and never mutated again.
	fields map[string]interface{}
}

// Tee returns a zapcore.Core that writes entries to both the provided core
// and to Stackdriver using the provided client and log ID.
//
// For fields to be written to Stackdriver, you must use the With() method on
// the returned Core rather than just on zc. (This function has no way of
// knowing about fields that already exist on zc. They will be preserved when
// writing to zc's existing destination, but not to Stackdriver.)
func Tee(zc zapcore.Core, client *gcl.Client, gclLogID string) zapcore.Core {
	gc := &Core{
		Logger:          client.Logger(gclLogID),
		SeverityMapping: DefaultSeverityMapping,
	}

	for l := zapcore.DebugLevel; l <= zapcore.FatalLevel; l++ {
		if zc.Enabled(l) {
			gc.MinLevel = l
			break
		}
	}

	return zapcore.NewTee(zc, gc)
}

// Enabled implements zapcore.Core.
func (c *Core) Enabled(l zapcore.Level) bool {
	return l >= c.MinLevel
}

// With implements zapcore.Core.
func (c *Core) With(newFields []zapcore.Field) zapcore.Core {
	return &Core{
		Logger:          c.Logger,
		SeverityMapping: c.SeverityMapping,
		MinLevel:        c.MinLevel,
		fields:          clone(c.fields, newFields),
	}
}

// Check implements zapcore.Core.
func (c *Core) Check(e zapcore.Entry, ce *zapcore.CheckedEntry) *zapcore.CheckedEntry {
	if c.Enabled(e.Level) {
		return ce.AddCore(e, c)
	}
	return ce
}

// Write implements zapcore.Core. It writes a log entry to Stackdriver.
//
// The "logger", "msg", "caller", and "stack" fields on the payload are
// populated from their respective values on the zapcore.Entry.
func (c *Core) Write(ze zapcore.Entry, newFields []zapcore.Field) error {
	severity, specified := c.SeverityMapping[ze.Level]
	if !specified {
		severity = gcl.Default
	}

	payload := clone(c.fields, newFields)

	payload["logger"] = ze.LoggerName
	payload["msg"] = ze.Message
	payload["caller"] = ze.Caller.String()
	payload["stack"] = ze.Stack

	c.Logger.Log(gcl.Entry{
		Timestamp: ze.Time,
		Severity:  severity,
		Payload:   payload,
	})

	return nil
}

// Sync implements zapcore.Core. It flushes the Core's Logger instance.
func (c *Core) Sync() error {
	if err := c.Logger.Flush(); err != nil {
		return newError("flushing Google Cloud logger: %v", err)
	}
	return nil
}

// DefaultSeverityMapping is the default mapping of zap's Levels to Google's
// Severities.
var DefaultSeverityMapping = map[zapcore.Level]gcl.Severity{
	zapcore.DebugLevel:  gcl.Debug,
	zapcore.InfoLevel:   gcl.Info,
	zapcore.WarnLevel:   gcl.Warning,
	zapcore.ErrorLevel:  gcl.Error,
	zapcore.DPanicLevel: gcl.Critical,
	zapcore.PanicLevel:  gcl.Critical,
	zapcore.FatalLevel:  gcl.Critical,
}

// clone creates a new field map without mutating the original.
func clone(orig map[string]interface{}, newFields []zapcore.Field) map[string]interface{} {
	clone := make(map[string]interface{})

	for k, v := range orig {
		clone[k] = v
	}

	for _, f := range newFields {
		clone[f.Key] = f.Interface
	}

	return clone
}

const packageName = "gcloudzap"

// newError calls fmt.Errorf() and prefixes the error with the packageName.
func newError(format string, args ...interface{}) error {
	return fmt.Errorf(packageName+": "+format, args)
}

// GoogleCloudLogger encapsulates the important methods of gcl.Logger
type GoogleCloudLogger interface {
	Flush() error
	Log(e gcl.Entry)
}
