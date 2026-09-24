package plugin

import (
	"encoding/json"
	"net/http"
	"testing"
)

// CPA has already checked the requested model's cooldown before sending these
// candidates. Their error status may describe an expired cooldown or a failure
// on another model; it must not stop the next request from recovering them.
func TestSchedulerPickRecoveredErrorCandidates(t *testing.T) {
	for _, mode := range []string{"no-group", "group", "global"} {
		t.Run(mode, func(t *testing.T) {
			configure := configureTestApp
			if mode == "global" {
				configure = configureGlobalSchedulerApp
			}
			app, plain := configure(t)
			t.Cleanup(app.Shutdown)
			request := SchedulerPickRequest{
				Provider: "codex",
				Model:    "gpt-5-codex",
				Options: SchedulerPickOptions{
					Headers: map[string][]string{"Authorization": {"Bearer " + plain}},
				},
				Candidates: []SchedulerAuthCandidate{
					{ID: "recovered-a", Provider: "codex", Status: "error", Weight: 3, Attributes: map[string]string{"plan_type": "team"}},
					{ID: "recovered-b", Provider: "codex", Status: "error", Weight: 1, Attributes: map[string]string{"plan_type": "team"}},
				},
			}
			if mode == "group" {
				request.Options.Metadata = map[string]any{"group": "team"}
			}
			if mode == "global" {
				request.Options.Metadata = map[string]any{"group": "plus"}
			}
			counts := map[string]int{}
			for i := 0; i < 8; i++ {
				response := schedulerPickForTest(t, app, request)
				counts[response.AuthID]++
			}
			if counts["recovered-a"] != 6 || counts["recovered-b"] != 2 {
				t.Fatalf("recovered candidates must retain 3:1 weights, got %v", counts)
			}
		})
	}
}

func TestSchedulerPickRecoveredErrorPreservesRestrictions(t *testing.T) {
	app, _ := configureTestApp(t)
	t.Cleanup(app.Shutdown)
	request := SchedulerPickRequest{
		Provider: "codex",
		Model:    "gpt-5-codex",
		Options:  SchedulerPickOptions{Metadata: map[string]any{"group": "team"}},
		Candidates: []SchedulerAuthCandidate{
			{ID: "disabled", Status: "disabled", Priority: 100, Weight: 1, Attributes: map[string]string{"plan_type": "team"}},
			{ID: "wrong-group", Status: "active", Priority: 100, Weight: 1, Attributes: map[string]string{"plan_type": "plus"}},
			{ID: "paused", Status: "error", Priority: 100, Weight: 0, Attributes: map[string]string{"plan_type": "team"}},
			{ID: "recovered", Status: "error", Priority: 10, Weight: 1, Attributes: map[string]string{"plan_type": "team"}},
			{ID: "fallback", Status: "active", Priority: 1, Weight: 100, Attributes: map[string]string{"plan_type": "team"}},
		},
	}
	if response := schedulerPickForTest(t, app, request); response.AuthID != "recovered" {
		t.Fatalf("must select highest eligible priority after recovery, got %+v", response)
	}

	// An error candidate from another group must never become a fallback.
	request.Candidates = []SchedulerAuthCandidate{
		{ID: "wrong-group", Status: "error", Weight: 1, Attributes: map[string]string{"plan_type": "plus"}},
	}
	rawRequest, err := json.Marshal(request)
	if err != nil {
		t.Fatal(err)
	}
	rawResponse, err := app.HandleMethod(MethodSchedulerPick, rawRequest)
	if err != nil {
		t.Fatal(err)
	}
	var envelope Envelope
	if err := json.Unmarshal(rawResponse, &envelope); err != nil {
		t.Fatal(err)
	}
	if envelope.OK || envelope.Error == nil || envelope.Error.Code != "auth_not_found" || envelope.Error.HTTPStatus != http.StatusServiceUnavailable {
		t.Fatalf("group isolation must still reject the request, got %+v", envelope)
	}
}

func TestSchedulerPickRecoveredErrorDefersNativeKey(t *testing.T) {
	app, _ := configureTestApp(t)
	t.Cleanup(app.Shutdown)
	rawRequest, err := json.Marshal(SchedulerPickRequest{
		Provider: "codex",
		Model:    "gpt-5-codex",
		Options:  SchedulerPickOptions{Headers: map[string][]string{"Authorization": {"Bearer native-key"}}},
		Candidates: []SchedulerAuthCandidate{
			{ID: "recovered", Provider: "codex", Status: "error"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	rawResponse, err := app.HandleMethod(MethodSchedulerPick, rawRequest)
	if err != nil {
		t.Fatal(err)
	}
	var response SchedulerPickResponse
	if err := unmarshalOK(rawResponse, &response); err != nil {
		t.Fatal(err)
	}
	if response.Handled || response.AuthID != "" {
		t.Fatalf("native key must still use CPA's scheduler, got %+v", response)
	}
}
