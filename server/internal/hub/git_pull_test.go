package hub

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"sync/atomic"
	"testing"
	"time"

	"github.com/chyroc/jarvis/server/internal/db"
	"github.com/chyroc/jarvis/server/internal/rpc"
)

func TestGitPullRouteAndPermissions(t *testing.T) {
	h := newTestHub(t)
	h.store = &tokenStore{memStore: *newMemStore(), rows: map[string]db.DeviceToken{}, rev: map[string]bool{}}
	hubSide, nodeSide := net.Pipe()
	hubPeer, nodePeer := rpc.New("h", hubSide), rpc.New("n", nodeSide)
	t.Cleanup(func() { hubSide.Close(); nodeSide.Close() })
	var calls atomic.Int32
	var failed atomic.Bool
	nodePeer.OnRequest = func(_ context.Context, method string, params json.RawMessage) (any, error) {
		calls.Add(1)
		var body map[string]any
		_ = json.Unmarshal(params, &body)
		if method != "git.pull" || body["cwd"] != "/work/sample" {
			return nil, fmt.Errorf("转发参数不正确：%s, %s", method, params)
		}
		if failed.Load() {
			return nil, fmt.Errorf("工作区有未提交的改动")
		}
		return map[string]any{"status": map[string]any{"is_repo": true, "behind": 0}, "updated": true}, nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	go func() { _ = hubPeer.Serve(ctx, hubSide) }()
	go func() { _ = nodePeer.Serve(ctx, nodeSide) }()
	m := newMachine(h, MachineConfig{ID: "mac", Name: "Sample computer"})
	m.peer = hubPeer
	h.byID["mac"] = m
	handler := h.routes()
	path := "/api/v1/machines/mac/git/pull"
	code, result := call(t, handler, "POST", path, h.cfg.Token, `{"cwd":"/work/sample"}`)
	if code != http.StatusOK || result["updated"] != true || calls.Load() != 1 {
		t.Fatalf("拉取路由失败：%d, %v, calls=%d", code, result, calls.Load())
	}
	code, _ = call(t, handler, "POST", path, h.cfg.Token, `not-json`)
	if code != http.StatusBadRequest || calls.Load() != 1 {
		t.Fatalf("无效 JSON 不应到达 node：%d, calls=%d", code, calls.Load())
	}
	_, watch := call(t, handler, "POST", "/api/v1/auth/tokens", h.cfg.Token, `{"name":"Watch","kind":"watch"}`)
	code, _ = call(t, handler, "POST", path, watch["token"].(string), `{"cwd":"/work/sample"}`)
	if code != http.StatusForbidden || calls.Load() != 1 {
		t.Fatalf("answer 角色不应执行拉取：%d, calls=%d", code, calls.Load())
	}
	failed.Store(true)
	code, result = call(t, handler, "POST", path, h.cfg.Token, `{"cwd":"/work/sample"}`)
	if code != http.StatusBadGateway || result["error"] == nil {
		t.Fatalf("node 错误应返回客户端：%d, %v", code, result)
	}
}
