// +build integration

package gcloudzap_test

import (
	"context"
	"os"
	"testing"
	"time"

	"cloud.google.com/go/logging/logadmin"
	"github.com/golang/protobuf/ptypes/struct"
	"github.com/jonstaryuk/gcloudzap"
)

// Integration testing requires Google Application Default Credentials which
// have read and write access to Stackdriver logs. If there is no
// authenticated gcloud CLI installation, this environment variable must be
// set: GOOGLE_APPLICATION_CREDENTIALS=path-to-credentials.json
//
// A project ID is also required: GCL_PROJECT_ID=test-project-id

const testLogID = "_gcloudzap_integration_test"

func TestIntegration(t *testing.T) {
	project, _ := os.LookupEnv("GCL_PROJECT_ID")
	if project == "" {
		t.Fatal("GCL_PROJECT_ID is blank")
	}

	logger, err := gcloudzap.NewProduction(project, testLogID)
	if err != nil {
		t.Fatal(err)
	}
	sugar := logger.Sugar()

	l1 := sugar.With("foo", "bar").With("baz", 123)
	l1.Info("Test1")
	if err := l1.Sync(); err != nil {
		t.Fatal(err)
	}

	ctx := context.Background()
	client, err := logadmin.NewClient(ctx, project)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()

	time.Sleep(2 * time.Second)

	es := client.Entries(ctx, logadmin.NewestFirst())
	e, err := es.Next()
	if err != nil {
		t.Fatal(err)
	}
	fields := e.Payload.(*structpb.Struct).GetFields()
	if fields["msg"].GetStringValue() != "Test1" {
		t.Errorf("msg has wrong value: %v", fields["msg"].String())
	}
	if fields["foo"].GetStringValue() != "bar" {
		t.Errorf("foo has wrong value: %v", fields["foo"].String())
	}
	if fields["baz"].GetNumberValue() != 123 {
		t.Errorf("baz has wrong value: %v", fields["baz"].String())
	}
	if t.Failed() {
		t.FailNow()
	}
}
