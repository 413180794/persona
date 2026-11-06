package extmgr

import (
	"bytes"
	"encoding/json"
	"testing"

	"github.com/nwverse/persona/sdk/go/wire"
)

func TestPagePresentationResultsAndReplayRetainNativePresence(t *testing.T) {
	h, process, _ := pageProcessManager(t)
	tree := pageOpenTest(t, h, pageWho)
	for _, result := range []json.RawMessage{
		json.RawMessage(`{"toast":"Saved"}`),
		json.RawMessage(`{"push":{"ui":"home","state":"fixture","title":"Detail"}}`),
		json.RawMessage(`{"pop":true}`),
		json.RawMessage(`{"open_url":"https://example.com/fixture"}`),
		json.RawMessage(`{"toast":"","pop":false,"lines":{"path":"fixture.go","start":1,"lines":[],"total":0}}`),
	} {
		pageFixture(t, h, "result", result)
		event := pageEvent(t, pageTree(h, tree.Session()), "no-ui", string(result))
		event.Interaction.EventID = randomHex(16)
		event.RequestID = "presentation"
		raw, err := pageCall(t, h, pageWho, event)
		if err != nil {
			t.Fatal(err)
		}
		if err := wire.ValidateJSON("UIStreamServerEnvelope", raw); err != nil {
			t.Fatalf("invalid receipt: %v %s", err, raw)
		}
		var receipt struct {
			Receipt struct {
				Result json.RawMessage `json:"result"`
			} `json:"receipt"`
		}
		if err := json.Unmarshal(raw, &receipt); err != nil {
			t.Fatal(err)
		}
		var want, got any
		_ = json.Unmarshal(result, &want)
		_ = json.Unmarshal(receipt.Receipt.Result, &got)
		a, _ := json.Marshal(want)
		b, _ := json.Marshal(got)
		if !bytes.Equal(a, b) {
			t.Fatalf("lost native result: want %s got %s", a, b)
		}
		event.RequestID = "retry-correlation"
		replay, err := pageCall(t, h, pageWho, event)
		if err != nil || !bytes.Contains(replay, []byte(`"request_id":"retry-correlation"`)) {
			t.Fatalf("replay correlation: %s %v", replay, err)
		}
		_ = json.Unmarshal(replay, &receipt)
		_ = json.Unmarshal(receipt.Receipt.Result, &got)
		b, _ = json.Marshal(got)
		if !bytes.Equal(a, b) {
			t.Fatalf("replay lost zero/empty fields: %s", replay)
		}
	}
	if pageStats(t, process)["writes"] != 5 {
		t.Fatal("result retries repeated writes")
	}
}

func TestPageUnsafeResultConsumesFenceWithoutPresentation(t *testing.T) {
	h, process, _ := pageProcessManager(t)
	tree := pageOpenTest(t, h, pageWho)
	for _, result := range []json.RawMessage{
		json.RawMessage(`{"open_url":"javascript:alert(1)"}`),
		json.RawMessage(`{"open_url":"https://user:password@example.com/"}`),
		json.RawMessage(`{"open_url":"persona://settings/extensions/other"}`),
		json.RawMessage(`{"push":{"ui":"other","state":""}}`),
		json.RawMessage(`{"push":{"page":"assistant-page"}}`),
		json.RawMessage(`{"view":{"sections":[]}}`),
		json.RawMessage(`{"items":[{"type":"html","html":"<script>fixture()</script>"}]}`),
		json.RawMessage(`{"authorize":true}`),
		json.RawMessage(`{"lines":{"path":"fixture.go","start":1,"lines":[],"script":"fixture()"}}`),
	} {
		pageFixture(t, h, "result", result)
		previous := pageTree(h, tree.Session())
		event := pageEvent(t, previous, "no-ui", randomHex(16))
		raw, err := pageCall(t, h, pageWho, event)
		if err != nil || !bytes.Contains(raw, []byte(`"status":"ambiguous"`)) || bytes.Contains(raw, []byte(`"result":`)) {
			t.Fatalf("unsafe result reached app: %s %v", raw, err)
		}
		if pageTree(h, tree.Session()).Version() != previous.Version()+1 {
			t.Fatal("invalid result did not consume exact fence")
		}
		replay, err := pageCall(t, h, pageWho, event)
		if err != nil || !bytes.Equal(raw, replay) {
			t.Fatalf("ambiguous replay: %s %v", replay, err)
		}
	}
	if pageStats(t, process)["writes"] != 9 {
		t.Fatal("unsafe result retry repeated callback")
	}
}

func TestPageReviewSubmitCorrelatesResultWithSavedValues(t *testing.T) {
	h, process, _ := pageProcessManager(t)
	pageFixture(t, h, "review", map[string]bool{"enabled": true})
	pageFixture(t, h, "result", json.RawMessage(`{"toast":"Review submitted"}`))
	tree := pageOpenTest(t, h, pageWho)
	action := tree.Snapshot().View.Review.Buttons[0].Action
	event := wire.UIStreamEvent{Protocol: 1, Session: tree.Session(), Epoch: tree.Epoch(), Version: tree.Version(), EventID: "review-submit", Key: tree.RootKey(), Action: action.ID, Event: "submit", Value: json.RawMessage(`{"summary":"Synthetic review","saved":"forged"}`)}
	raw, err := pageCall(t, h, pageWho, wire.UIStreamRequest{Request: "event", Protocol: 1, RequestID: "review-correlation", Interaction: &event})
	if err != nil || !bytes.Contains(raw, []byte(`"toast":"Review submitted"`)) || !bytes.Contains(raw, []byte(`"event_id":"review-submit"`)) {
		t.Fatalf("review result: %s %v", raw, err)
	}
	var values map[string]string
	if err := process.Call(t.Context(), "test.values", nil, &values); err != nil {
		t.Fatal(err)
	}
	if values["saved"] != "native" || values["summary"] != "Synthetic review" {
		t.Fatalf("saved values lost: %v", values)
	}
}

func TestPageLegacyPresentationAndStaleRetry(t *testing.T) {
	h, process, _ := pageProcessManager(t)
	var in uiRequest
	in.Context.UIVersion = 6
	var raw json.RawMessage
	in.nativeResult = &raw
	result, err := h.uiCall(t.Context(), "example-page", "home", in)
	if err != nil {
		t.Fatal(err)
	}
	in.State = result.View.State
	for _, item := range result.View.Sections[0].Items {
		if item.Key == "no-ui" {
			in.Action = item.Action.ID
		}
	}
	pageFixture(t, h, "result", json.RawMessage(`{"toast":"Saved","open_url":"https://example.com","push":{"ui":"home","state":"fixture"},"pop":false,"lines":{"path":"fixture.go","start":1,"lines":[]}}`))
	result, err = h.uiCall(t.Context(), "example-page", "home", in)
	if err != nil || result.Toast != "Saved" || result.Push == nil || result.OpenURL != "https://example.com" || result.Lines == nil {
		t.Fatalf("HTTP lost presentation: %s %v", raw, err)
	}
	if !bytes.Contains(raw, []byte(`"pop":false`)) || !bytes.Contains(raw, []byte(`"lines":[]`)) {
		t.Fatalf("HTTP lost presence: %s", raw)
	}
	if _, err := h.uiCall(t.Context(), "example-page", "home", in); err == nil {
		t.Fatal("stale HTTP retry repeated presentation")
	}
	if pageStats(t, process)["writes"] != 1 {
		t.Fatal("HTTP retry repeated mutation")
	}
}
