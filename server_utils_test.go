package server_utils_test

import (
	"bytes"
	"compress/gzip"
	"context"
	"fmt"
	"io/ioutil"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"cloud.google.com/go/errorreporting"
	"cloud.google.com/go/logging"
	"github.com/gorilla/handlers"
	"github.com/gorilla/mux"

	base "github.com/savannahghi/go_utils"
	"github.com/savannahghi/server_utils"
	log "github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
)

func TestSentry(t *testing.T) {
	tests := []struct {
		name    string
		wantErr bool
	}{
		{
			name:    "default case",
			wantErr: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := server_utils.Sentry(); (err != nil) != tt.wantErr {
				t.Errorf("Sentry() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestErrorMap(t *testing.T) {
	err := fmt.Errorf("test error")
	errMap := server_utils.ErrorMap(err)
	if errMap["error"] == "" {
		t.Errorf("empty error key in errMap")
	}
	if errMap["error"] != "test error" {
		t.Errorf("expected the error value to be '%s', got '%s'", "test error", errMap["error"])
	}
}

func TestRequestDebugMiddleware(t *testing.T) {
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {})

	mw := server_utils.RequestDebugMiddleware()
	h := mw(next)

	rw := httptest.NewRecorder()
	reader := bytes.NewBuffer([]byte("sample"))
	request := httptest.NewRequest(http.MethodPost, "/", reader)
	h.ServeHTTP(rw, request)

	rw1 := httptest.NewRecorder()
	reader1 := ioutil.NopCloser(bytes.NewBuffer([]byte("will be closed")))
	err := reader1.Close()
	assert.Nil(t, err)
	req1 := httptest.NewRequest(http.MethodPost, "/", reader1)
	h.ServeHTTP(rw1, req1)
}

func TestLogStartupError(t *testing.T) {
	type args struct {
		ctx context.Context
		err error
	}
	tests := []struct {
		name string
		args args
	}{
		{
			name: "good case",
			args: args{
				ctx: context.Background(),
				err: fmt.Errorf("this is a test error"),
			},
		},
		{
			name: "nil error",
			args: args{
				ctx: context.Background(),
				err: nil,
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server_utils.LogStartupError(tt.args.ctx, tt.args.err)
		})
	}
}

func TestDecodeJSONToTargetStruct(t *testing.T) {
	type target struct {
		A string `json:"a,omitempty"`
	}
	targetStruct := target{}

	type args struct {
		w            http.ResponseWriter
		r            *http.Request
		targetStruct interface{}
	}
	tests := []struct {
		name string
		args args
	}{
		{
			name: "good case",
			args: args{
				w: httptest.NewRecorder(),
				r: httptest.NewRequest(
					http.MethodGet,
					"/",
					bytes.NewBuffer([]byte(
						"{\"a\":\"1\"}",
					)),
				),
				targetStruct: &targetStruct,
			},
		},
		{
			name: "invalid / failed decode",
			args: args{
				w: httptest.NewRecorder(),
				r: httptest.NewRequest(
					http.MethodGet,
					"/",
					nil,
				),
				targetStruct: &targetStruct,
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server_utils.DecodeJSONToTargetStruct(tt.args.w, tt.args.r, tt.args.targetStruct)
		})
	}
}

func Test_convertStringToInt(t *testing.T) {
	tests := map[string]struct {
		val                string
		rw                 *httptest.ResponseRecorder
		expectedStatusCode int
		expectedResponse   string
	}{
		"successful_conversion": {
			val:                "768",
			rw:                 httptest.NewRecorder(),
			expectedStatusCode: 200,
		},
		"failed_conversion": {
			val:                "not an int",
			rw:                 httptest.NewRecorder(),
			expectedStatusCode: http.StatusInternalServerError,
			expectedResponse:   "{\"error\":\"strconv.Atoi: parsing \\\"not an int\\\": invalid syntax\"}",
		},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			server_utils.ConvertStringToInt(tc.rw, tc.val)
			assert.Equal(t, tc.expectedStatusCode, tc.rw.Code)
			assert.Equal(t, tc.expectedResponse, tc.rw.Body.String())
		})
	}
}

func Test_StackDriver_Setup(t *testing.T) {
	errorClient := server_utils.StackDriver(context.Background())
	err := fmt.Errorf("test error")
	if errorClient != nil {
		errorClient.Report(errorreporting.Entry{
			Error: err,
		})
	}
}

func TestStackDriver(t *testing.T) {
	ctx := context.Background()
	type args struct {
		ctx context.Context
	}
	tests := []struct {
		name string
		args args
	}{
		{
			name: "happy case",
			args: args{
				ctx: ctx,
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := server_utils.StackDriver(tt.args.ctx)
			assert.NotNil(t, got)
		})
	}
}

func TestWriteJSONResponse(t *testing.T) {
	unmarshallable := make(chan string) // can't be marshalled to JSON
	errReq := base.NewErrorResponseWriter(fmt.Errorf("ka-boom"))

	type args struct {
		w      http.ResponseWriter
		source interface{}
		status int
	}
	tests := []struct {
		name       string
		args       args
		wantStatus int
	}{
		{
			name: "happy case - successful write",
			args: args{
				w:      httptest.NewRecorder(),
				source: map[string]string{"test_key": "test_value"},
				status: http.StatusOK,
			},
			wantStatus: 200,
		},
		{
			name: "unmarshallable content",
			args: args{
				w:      httptest.NewRecorder(),
				source: unmarshallable,
				status: http.StatusInternalServerError,
			},
			wantStatus: http.StatusInternalServerError,
		},
		{
			name: "closed recorder",
			args: args{
				w:      errReq,
				source: map[string]string{"test_key": "test_value"},
				status: http.StatusOK,
			},
			wantStatus: http.StatusInternalServerError,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server_utils.WriteJSONResponse(tt.args.w, tt.args.source, tt.args.status)

			rec, ok := tt.args.w.(*httptest.ResponseRecorder)
			if ok {
				assert.NotNil(t, rec)
				assert.Equal(t, tt.wantStatus, rec.Code)
			}
			if !ok {
				rec, ok := tt.args.w.(*base.ErrorResponseWriter)
				assert.True(t, ok)
				assert.NotNil(t, rec)
			}
		})
	}
}

func Test_closeStackDriverLoggingClient(t *testing.T) {
	projectID := base.MustGetEnvVar(server_utils.GoogleCloudProjectIDEnvVarName)
	loggingClient, err := logging.NewClient(context.Background(), projectID)
	assert.Nil(t, err)

	type args struct {
		loggingClient *logging.Client
	}
	tests := []struct {
		name string
		args args
	}{
		{
			name: "happy case - successful",
			args: args{
				loggingClient: loggingClient,
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server_utils.CloseStackDriverLoggingClient(tt.args.loggingClient)
		})
	}
}

func Test_closeStackDriverErrorClient(t *testing.T) {
	projectID := base.MustGetEnvVar(server_utils.GoogleCloudProjectIDEnvVarName)
	errorClient, err := errorreporting.NewClient(context.Background(), projectID, errorreporting.Config{
		ServiceName: server_utils.AppName,
		OnError: func(err error) {
			log.WithFields(log.Fields{
				"project ID":   projectID,
				"service name": server_utils.AppName,
				"error":        err,
			}).Info("Unable to initialize error client")
		},
	})
	assert.Nil(t, err)

	type args struct {
		errorClient *errorreporting.Client
	}
	tests := []struct {
		name string
		args args
	}{
		{
			name: "happy case - successful",
			args: args{
				errorClient: errorClient,
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server_utils.CloseStackDriverErrorClient(tt.args.errorClient)
		})
	}
}

func TestStartTestServer(t *testing.T) {

	ctx := context.Background()
	srv, baseURL, serverErr := server_utils.StartTestServer(ctx, healthCheckServer, []string{
		"http://localhost:5000",
	})
	if serverErr != nil {
		t.Errorf("Unable to start test server %s", serverErr)
		return
	}
	defer srv.Close()
	if srv == nil {
		t.Errorf("nil test server %s", serverErr)
		return
	}
	if baseURL == "" {
		t.Errorf("empty base url %s", serverErr)
		return
	}
}

func healthCheckRouter() (*mux.Router, error) {
	r := mux.NewRouter() // gorilla mux
	r.Use(
		handlers.RecoveryHandler(
			handlers.PrintRecoveryStack(true),
			handlers.RecoveryLogger(log.StandardLogger()),
		),
	) // recover from panics by writing a HTTP error

	r.Use(server_utils.RequestDebugMiddleware())
	r.Path("/health").HandlerFunc(server_utils.HealthStatusCheck)

	return r, nil
}

func healthCheckServer(ctx context.Context, port int, allowedOrigins []string) *http.Server {
	// start up the router
	r, err := healthCheckRouter()
	if err != nil {
		server_utils.LogStartupError(ctx, err)
	}

	// start the server
	addr := fmt.Sprintf(":%d", port)
	h := handlers.CompressHandlerLevel(r, gzip.BestCompression)
	h = handlers.CORS(
		handlers.AllowedOrigins(allowedOrigins),
		handlers.AllowCredentials(),
		handlers.AllowedMethods([]string{"OPTIONS", "GET", "POST"}),
	)(h)
	h = handlers.CombinedLoggingHandler(os.Stdout, h)
	h = handlers.ContentTypeHandler(h, "application/json")
	srv := &http.Server{
		Handler: h,
		Addr:    addr,
	}
	log.Infof("Server running at port %v", addr)
	return srv

}
