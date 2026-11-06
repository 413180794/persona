package hub

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/nwverse/persona/server/internal/assistant"
	"github.com/nwverse/persona/server/internal/assistantwire"
)

// The model configuration's own tests are the assistant's (internal/assistant/models_test.go); these are the hub's
// end of it: the routes relayed, the task purposes as the assistant reports them.

type modelTestStore struct {
	sharedStore
	mu     sync.Mutex
	values map[string]json.RawMessage
	fail   bool
}

func (s *modelTestStore) Setting(_ context.Context, key string, out any) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.fail {
		return false, errors.New("database unavailable")
	}
	raw, ok := s.values[key]
	if !ok {
		return false, nil
	}
	return true, json.Unmarshal(raw, out)
}
func (s *modelTestStore) PutSetting(_ context.Context, key string, v any) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.fail {
		return errors.New("database unavailable")
	}
	raw, err := json.Marshal(v)
	if err == nil {
		s.values[key] = raw
	}
	return err
}

// modelTestHub is a hub whose mux has the model configuration's routes, relayed to the assistant (connectAssistant
// connects it: the model configuration is its, models.go there).
func modelTestHub(t *testing.T) (*Hub, *http.ServeMux) {
	h := newTestHub(t)
	h.store = &modelTestStore{values: map[string]json.RawMessage{}}
	mux := http.NewServeMux()
	h.assistantRoutesRelayed(mux)
	return h, mux
}
func modelRequest(mux http.Handler, method, id, body string) *httptest.ResponseRecorder {
	path := "/api/v1/model-config"
	if id != "" {
		path += "/purposes/" + id
	}
	r := httptest.NewRecorder()
	mux.ServeHTTP(r, httptest.NewRequest(method, path, strings.NewReader(body)))
	return r
}

func TestTaskModelConfigurationSurvivesOrdinaryTaskSave(t *testing.T) {
	h, mux := modelTestHub(t)
	setManagerJSON(t, h, "assistant.launcher", "claude")
	seedTasks(t, h, assistant.Task{ID: "one", Title: "任务", Prompt: "x", Repeat: "interval", EveryMinutes: 30, Enabled: true})
	connectAssistant(t, h)
	r := modelRequest(mux, "PATCH", "assistant", `{"revision":0,"provider":"launcher.claude","model":"exact","effort":"high"}`)
	if r.Code != 200 {
		t.Fatal(r.Body.String())
	}
	request := httptest.NewRequest("PUT", "/api/v1/assistant/tasks/one", strings.NewReader(`{"title":"新任务","prompt":"y","repeat":"interval","every_minutes":45,"enabled":true}`))
	result := httptest.NewRecorder()
	mux.ServeHTTP(result, request)
	if result.Code != 200 {
		t.Fatal(result.Body.String())
	}
	var detail struct {
		Purpose struct {
			Effective assistantwire.ModelBinding `json:"effective"`
		} `json:"purpose"`
	}
	r = modelRequest(mux, "GET", "task.one", "")
	if err := json.Unmarshal(r.Body.Bytes(), &detail); err != nil || detail.Purpose.Effective.Model != "exact" || detail.Purpose.Effective.Effort != "high" {
		t.Fatalf("ordinary task edit overwrote model: %s %v", r.Body.String(), err)
	}
}
