package server

import (
	"bytes"
	"encoding/json"
	"errors"
	"io/fs"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"testing/fstest"
)

func TestStatusRecorderKeepsFirstStatus(t *testing.T) {
	recorder := &statusRecorder{ResponseWriter: httptest.NewRecorder()}
	recorder.WriteHeader(http.StatusTeapot)
	recorder.WriteHeader(http.StatusOK)
	if _, err := recorder.Write([]byte("abc")); err != nil {
		t.Fatal(err)
	}
	if recorder.status != http.StatusTeapot {
		t.Errorf("status = %d", recorder.status)
	}
	if recorder.bytes != 3 {
		t.Errorf("bytes = %d", recorder.bytes)
	}
}

func TestStatusRecorderImplicitOK(t *testing.T) {
	recorder := &statusRecorder{ResponseWriter: httptest.NewRecorder()}
	if _, err := recorder.Write([]byte("x")); err != nil {
		t.Fatal(err)
	}
	if recorder.status != http.StatusOK {
		t.Errorf("status = %d", recorder.status)
	}
}

func TestStatusRecorderSupportsResponseController(t *testing.T) {
	inner := httptest.NewRecorder()
	recorder := &statusRecorder{ResponseWriter: inner}
	if err := http.NewResponseController(recorder).Flush(); err != nil {
		t.Fatalf("flush through Unwrap failed: %v", err)
	}
	if !inner.Flushed {
		t.Error("inner writer was not flushed")
	}
}

func TestAccessLogReportsOKWhenHandlerWritesNothing(t *testing.T) {
	var out bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&out, nil))
	handler := withAccessLog(logger, http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))

	handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/x", nil))

	var record map[string]any
	if err := json.Unmarshal(bytes.TrimSpace(out.Bytes()), &record); err != nil {
		t.Fatalf("log is not JSON: %v", err)
	}
	if record["status"] != float64(200) {
		t.Errorf("status = %v", record["status"])
	}
}

type brokenIndexFS struct{ fstest.MapFS }

func (b brokenIndexFS) Open(name string) (fs.File, error) {
	if name == indexFile {
		return failingFile{}, nil
	}
	return b.MapFS.Open(name)
}

func (b brokenIndexFS) Stat(name string) (fs.FileInfo, error) {
	if name == indexFile {
		return failingFile{}.Stat()
	}
	return b.MapFS.Stat(name)
}

type failingFile struct{}

func (failingFile) Stat() (fs.FileInfo, error) {
	return fstest.MapFS{indexFile: {Data: []byte("x")}}.Stat(indexFile)
}
func (failingFile) Read([]byte) (int, error) { return 0, errors.New("disk failure") }
func (failingFile) Close() error             { return nil }

func TestSPAReportsUnreadableIndex(t *testing.T) {
	handler := NewHandler(Options{Assets: brokenIndexFS{MapFS: fstest.MapFS{}}})

	recorder := do(t, handler, http.MethodGet, "/route")
	if recorder.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d", recorder.Code)
	}
	var body errorBody
	decodeJSON(t, recorder, &body)
	if body.Error != "internal_error" {
		t.Errorf("body = %+v", body)
	}
}
