package gcloudzap

import (
	"sync"
	"testing"
	"time"

	gcl "cloud.google.com/go/logging"
	"github.com/google/go-cmp/cmp"
	"go.uber.org/zap/zapcore"
)

func TestCoreFields(t *testing.T) {
	c1 := &Core{}
	c2 := c1.With([]zapcore.Field{
		{Key: "foo", Interface: "bar"},
	}).(*Core)
	c3 := c2.With([]zapcore.Field{
		{Key: "baz", Interface: "qux"},
	}).(*Core)

	if len(c1.fields) != 0 {
		t.Error("c1 should not have any fields")
	}

	expected := map[string]interface{}{"foo": "bar"}
	if diff := cmp.Diff(expected, c2.fields); diff != "" {
		t.Error(diff)
	}

	expected = map[string]interface{}{"foo": "bar", "baz": "qux"}
	if diff := cmp.Diff(expected, c3.fields); diff != "" {
		t.Error(diff)
	}
}

func TestCoreWrite(t *testing.T) {
	l := &testLogger{}
	ts := time.Now()

	c1 := &Core{Logger: l, SeverityMapping: DefaultSeverityMapping}
	c2 := c1.With([]zapcore.Field{
		{Key: "foo", Interface: "bar"},
	})

	e := zapcore.Entry{
		Message:    "hello",
		LoggerName: "test",
		Level:      zapcore.WarnLevel,
		Time:       ts,
	}
	fields := []zapcore.Field{{Key: "baz", Interface: "qux"}}
	if err := c2.Write(e, fields); err != nil {
		t.Error(err)
	}
	expected := []gcl.Entry{
		{
			Timestamp: ts,
			Severity:  gcl.Warning,
			Payload: map[string]interface{}{
				"msg":    "hello",
				"logger": "test",
				"caller": "undefined",
				"stack":  "",
				"foo":    "bar",
				"baz":    "qux",
			},
		},
	}
	if diff := cmp.Diff(expected, l.entries); diff != "" {
		t.Error(diff)
	}

	fields = []zapcore.Field{{Key: "asdf", Interface: "asdf"}}
	if err := c2.Write(e, fields); err != nil {
		t.Error(err)
	}
	expected = append(expected, gcl.Entry{
		Timestamp: ts,
		Severity:  gcl.Warning,
		Payload: map[string]interface{}{
			"msg":    "hello",
			"logger": "test",
			"caller": "undefined",
			"stack":  "",
			"foo":    "bar",
			"asdf":   "asdf",
		},
	})
	if diff := cmp.Diff(expected, l.entries); diff != "" {
		t.Error(diff)
	}
}

func TestConcurrentCoreWrite(t *testing.T) {
	l := &testLogger{}
	c := &Core{Logger: l}

	wg := sync.WaitGroup{}
	for i := 0; i < 100; i++ {
		wg.Add(1)
		index := i // capture loop variable
		go func() {
			fields := []zapcore.Field{{Key: "i", Interface: index}}
			if err := c.Write(zapcore.Entry{}, fields); err != nil {
				t.Error(err)
			}
			wg.Done()
		}()
	}
	wg.Wait()

	found := map[int]bool{}
	for _, e := range l.entries {
		i := e.Payload.(map[string]interface{})["i"].(int)
		found[i] = true
	}

	if len(found) != 100 {
		t.Error(found)
	}
}

func BenchmarkCoreClone(b *testing.B) {
	c := &Core{}
	for i := 0; i < b.N; i++ {
		c.With([]zapcore.Field{
			{Key: "i", Interface: i},
			{Key: "foo", Interface: "bar"},
			{Key: "bar", Interface: "baz"},
			{Key: "longstring", Interface: "asdfasdfasdfasdfasdfasdfasdfasdfasdfasdfasdfasdfasdfasdfasdf"},
			{Key: "longstring2", Interface: "asdfasdfasdfasdfasdfasdfasdfasdfasdfasdfasdfasdfasdfasdfasdf"},
			{Key: "longstring3", Interface: "asdfasdfasdfasdfasdfasdfasdfasdfasdfasdfasdfasdfasdfasdfasdf"},
		})
	}
}

func TestCoreSync(t *testing.T) {
	l := &testLogger{}
	c := &Core{Logger: l}

	if err := c.Sync(); err != nil {
		t.Error(err)
	}
	if !l.flushed {
		t.Error("Logger not flushed")
	}
}

func TestCoreLevels(t *testing.T) {
	c := &Core{MinLevel: zapcore.InfoLevel}
	if c.Enabled(zapcore.DebugLevel) {
		t.Error("Debug level must not be enabled with MinLevel set to Info")
	}

	c = &Core{MinLevel: zapcore.WarnLevel}
	if !c.Enabled(zapcore.ErrorLevel) {
		t.Error("Error level must be enabled with MinLevel set to Warn")
	}
}

type testLogger struct {
	entries []gcl.Entry
	mu      sync.Mutex
	flushed bool
}

func (t *testLogger) Flush() error {
	t.flushed = true
	return nil
}

func (t *testLogger) Log(e gcl.Entry) {
	// simulate adding entries to a buffer
	t.mu.Lock()
	t.entries = append(t.entries, e)
	t.mu.Unlock()
}

func BenchmarkCoreWrite(b *testing.B) {
	c := &Core{
		Logger: &testLogger{},
		fields: map[string]interface{}{
			"foo":         "bar",
			"longstring":  "asdfasdfasdfasdfasdfasdfasdfasdfasdfasdfasdfasdfasdfasdfasdf",
			"longstring2": "asdfasdfasdfasdfasdfasdfasdfasdfasdfasdfasdfasdfasdfasdfasdf",
			"longstring3": "asdfasdfasdfasdfasdfasdfasdfasdfasdfasdfasdfasdfasdfasdfasdf",
		},
	}
	for i := 0; i < b.N; i++ {
		e := zapcore.Entry{
			Message:    "hello",
			LoggerName: "benchmark",
			Level:      zapcore.WarnLevel,
		}
		c.Write(e, []zapcore.Field{})
	}
}
