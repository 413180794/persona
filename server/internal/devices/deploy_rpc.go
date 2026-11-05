package devices

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/nwverse/persona/server/internal/proto"
	"github.com/nwverse/persona/server/internal/rpc"
)

// The desired deployment survives a hub restart. Only the node's local recovery
// transaction lives beside its executable; hub state stays in Postgres settings.
type nodeDeployRecord struct {
	Target string    `json:"target"`
	State  string    `json:"state"` // queued | succeeded | failed
	Phase  string    `json:"phase,omitempty"`
	Error  string    `json:"error,omitempty"`
	At     time.Time `json:"at"`
}

func (d *Devices) deployContext() context.Context {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.ctx != nil {
		return d.ctx
	}
	return context.Background()
}

// Called with m.mu held: use the node protocol when SSH cannot reach the owner.
func (m *Machine) rpcDeploymentLocked() bool {
	return m.cfg.Kind == kindLink || m.cfg.Kind == kindService || m.derived || m.via == viaHub
}

func (m *Machine) canDeployLocked() (bool, string) {
	if m.cfg.Kind == "local" {
		return false, "hub 所在的机器跟着 hub 一起更新"
	}
	if m.rpcDeploymentLocked() && !m.info.NodeUpdate {
		if m.info.NodeUpdateReason != "" {
			return false, m.info.NodeUpdateReason
		}
		return false, "请先在这台电脑上升级一次 node 服务，之后即可在这里远程更新"
	}
	if !m.online {
		return false, "这台电脑不在线，连上以后再更新"
	}
	return true, ""
}

func (m *Machine) deployRecordKey() string { return "node.deploy." + m.cfg.ID }

func (m *Machine) saveDeployRecord(ctx context.Context, record nodeDeployRecord) error {
	if m.d.opts.Store == nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	return m.d.opts.Store.PutSetting(ctx, m.deployRecordKey(), record)
}

func (m *Machine) deploymentPhase(ctx context.Context, record *nodeDeployRecord, phase string) error {
	record.Phase, record.At = phase, time.Now().UTC()
	if err := m.saveDeployRecord(ctx, *record); err != nil {
		return fmt.Errorf("保存更新进度失败：%w", err)
	}
	m.mu.Lock()
	m.deployPhase = phase
	m.mu.Unlock()
	m.d.changed()
	return nil
}

func (m *Machine) startRPCDeployment(bin string, saved *nodeDeployRecord) error {
	m.mu.Lock()
	if m.deploying {
		m.mu.Unlock()
		return nil
	}
	allowed, reason := m.canDeployLocked()
	if !allowed {
		m.mu.Unlock()
		return errors.New(reason)
	}
	m.deploying, m.deployErr, m.deployPhase = true, "", "waiting_idle"
	m.mu.Unlock()
	record := nodeDeployRecord{Target: m.d.hubVersion(), State: "queued", Phase: "waiting_idle", At: time.Now().UTC()}
	if saved != nil {
		record = *saved
	}
	ctx := m.d.deployContext()
	if err := m.saveDeployRecord(ctx, record); err != nil {
		m.mu.Lock()
		m.deploying, m.deployPhase = false, ""
		m.mu.Unlock()
		return fmt.Errorf("保存更新任务失败：%w", err)
	}
	m.d.changed()
	go m.runRPCDeployment(ctx, bin, record)
	return nil
}

func (m *Machine) runRPCDeployment(ctx context.Context, bin string, record nodeDeployRecord) {
	err := m.performRPCDeployment(ctx, bin, &record)
	if ctx.Err() == nil {
		record.State, record.Phase, record.Error, record.At = "succeeded", "", "", time.Now().UTC()
		if err != nil {
			record.State, record.Error = "failed", err.Error()
		}
		if saveErr := m.saveDeployRecord(ctx, record); saveErr != nil {
			m.d.logf("[%s] save node deployment: %v", m.cfg.ID, saveErr)
			if err == nil {
				err = fmt.Errorf("更新已确认，但保存结果失败：%w", saveErr)
			}
		}
	}
	m.mu.Lock()
	m.deploying, m.deployPhase = false, ""
	if err != nil && ctx.Err() == nil {
		m.deployErr = err.Error()
	}
	m.mu.Unlock()
	if err != nil {
		m.d.logf("[%s] node update: %v", m.cfg.ID, err)
	} else {
		m.d.logf("[%s] node update confirmed: %s", m.cfg.ID, record.Target)
	}
	m.d.changed()
}

func (m *Machine) performRPCDeployment(ctx context.Context, bin string, record *nodeDeployRecord) error {
	m.mu.Lock()
	already := m.online && m.info.Version == record.Target
	m.mu.Unlock()
	if already {
		return m.confirmNodeDeployment(ctx, record)
	}
	for {
		if err := m.deploymentPhase(ctx, record, "waiting_idle"); err != nil {
			return err
		}
		if err := m.waitNodeIdle(ctx); err != nil {
			return err
		}
		// Waiting for idle has no deadline. Transfer/restart does, including a
		// busy race at commit, which releases the staging transaction and waits again.
		attempt, cancel := context.WithTimeout(ctx, 15*time.Minute)
		err := m.sendNodeUpdate(attempt, bin, record)
		cancel()
		var remote *rpc.Error
		if errors.As(err, &remote) && remote.Code == proto.CodeNodeUpdateBusy {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(2 * time.Second):
			}
			continue
		}
		return err
	}
}

func (m *Machine) waitNodeIdle(ctx context.Context) error {
	for {
		var list struct {
			Sessions []proto.Session `json:"sessions"`
		}
		check, cancel := context.WithTimeout(ctx, 15*time.Second)
		err := m.call(check, "sessions.list", map[string]any{}, &list)
		cancel()
		if err != nil {
			return fmt.Errorf("检查电脑是否空闲失败：%w", err)
		}
		m.setSessions(list.Sessions)
		busy := m.machinePending() // a question of its agents waits on the phone
		for _, s := range list.Sessions {
			if s.State == proto.StateWorking || s.State == proto.StateNeedsYou || s.Status == proto.StatusWorking || s.PendingInteractions > 0 || s.Starting || len(s.Background) > 0 {
				busy = true
				break
			}
			for _, sub := range s.Subagents {
				if sub.Running {
					busy = true
					break
				}
			}
		}
		if !busy {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(2 * time.Second):
		}
	}
}

func (m *Machine) sendNodeUpdate(ctx context.Context, bin string, record *nodeDeployRecord) error {
	f, err := os.Open(bin)
	if err != nil {
		return fmt.Errorf("打开 node 更新包失败：%w", err)
	}
	defer f.Close()
	stat, err := f.Stat()
	if err != nil {
		return err
	}
	hash := sha256.New()
	if _, err := io.Copy(hash, f); err != nil {
		return err
	}
	if err := m.deploymentPhase(ctx, record, "transferring"); err != nil {
		return err
	}
	m.mu.Lock()
	peer := m.peer
	m.mu.Unlock()
	if peer == nil {
		return errOffline
	}
	var begin struct {
		ID     string `json:"id"`
		Offset int64  `json:"offset"`
	}
	if err := peer.Call(ctx, "node.update.begin", map[string]any{
		"size": stat.Size(), "sha256": hex.EncodeToString(hash.Sum(nil)), "version": record.Target,
	}, &begin); err != nil {
		return fmt.Errorf("准备 node 更新失败：%w", err)
	}
	if begin.ID == "" {
		return errors.New("node 返回了无效的更新事务")
	}
	committed := false
	defer func() {
		if !committed {
			abort, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
			defer cancel()
			_ = peer.Call(abort, "node.update.abort", map[string]any{"id": begin.ID}, nil)
		}
	}()
	if begin.Offset < 0 || begin.Offset > stat.Size() {
		return errors.New("node 返回了无效的更新传输位置")
	}
	if _, err := f.Seek(begin.Offset, io.SeekStart); err != nil {
		return err
	}
	buf := make([]byte, proto.UploadChunkBytes)
	for offset := begin.Offset; offset < stat.Size(); {
		n, err := io.ReadFull(f, buf[:min(int64(len(buf)), stat.Size()-offset)])
		if err != nil {
			return fmt.Errorf("读取 node 更新包失败：%w", err)
		}
		var chunk struct {
			Offset int64 `json:"offset"`
		}
		if err := peer.Call(ctx, "node.update.chunk", map[string]any{"id": begin.ID, "offset": offset, "data": buf[:n]}, &chunk); err != nil {
			return fmt.Errorf("传输 node 更新失败：%w", err)
		}
		offset += int64(n)
		if chunk.Offset != offset {
			return errors.New("node 确认的更新传输位置不正确")
		}
	}
	if err := m.deploymentPhase(ctx, record, "restarting"); err != nil {
		return err
	}
	commit, cancelCommit := context.WithTimeout(ctx, 30*time.Second)
	err = peer.Call(commit, "node.update.commit", map[string]any{"id": begin.ID}, nil)
	cancelCommit()
	if err != nil {
		var remote *rpc.Error
		if errors.As(err, &remote) {
			return fmt.Errorf("安装 node 更新失败：%w", err)
		}
		// A lost commit response is ambiguous: the node may already have
		// replaced itself. Never report success until it reconnects and confirms.
		m.d.logf("[%s] node update commit response lost, checking reconnect: %v", m.cfg.ID, err)
	}
	committed = true
	return m.confirmNodeDeployment(ctx, record)
}

func (m *Machine) confirmNodeDeployment(ctx context.Context, record *nodeDeployRecord) error {
	if err := m.deploymentPhase(ctx, record, "confirming"); err != nil {
		return err
	}
	deadline, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	for {
		m.mu.Lock()
		peer, online, version := m.peer, m.online, m.info.Version
		m.mu.Unlock()
		if online && peer != nil && version == record.Target {
			confirm, ccancel := context.WithTimeout(deadline, 10*time.Second)
			err := peer.Call(confirm, "node.update.confirm", map[string]any{"version": record.Target}, nil)
			ccancel()
			if err == nil {
				return nil
			}
		}
		select {
		case <-deadline.Done():
			if ctx.Err() != nil {
				return ctx.Err()
			}
			return errors.New("新 node 未在两分钟内重新上线并确认版本；本机恢复机制会在确认超时后回滚，请查看电脑状态后重试")
		case <-time.After(time.Second):
		}
	}
}

// Confirm a healthy replacement even if the hub restarted during transfer. An
// unconfirmed target must never remain installed just because a worker vanished.
func (m *Machine) nodeUpdateConnected(ctx context.Context, peer *rpc.Peer, info proto.NodeInfo) {
	if info.NodeUpdate && info.Version == m.d.hubVersion() {
		confirm, cancel := context.WithTimeout(ctx, 10*time.Second)
		err := peer.Call(confirm, "node.update.confirm", map[string]any{"version": info.Version}, nil)
		cancel()
		if err != nil {
			m.d.logf("[%s] confirm connected node update: %v", m.cfg.ID, err)
		}
	}
	if m.d.opts.Store == nil {
		return
	}
	m.mu.Lock()
	active, linked := m.deploying, m.rpcDeploymentLocked()
	m.mu.Unlock()
	if active || !linked {
		return
	}
	var record nodeDeployRecord
	load, cancel := context.WithTimeout(ctx, 10*time.Second)
	found, err := m.d.opts.Store.Setting(load, m.deployRecordKey(), &record)
	cancel()
	if err != nil || !found {
		if err != nil {
			m.d.logf("[%s] load pending node update: %v", m.cfg.ID, err)
		}
		return
	}
	if record.State != "queued" {
		if record.State == "failed" {
			m.mu.Lock()
			m.deployErr = record.Error
			m.mu.Unlock()
			m.d.changed()
		}
		return
	}
	if record.Target != m.d.hubVersion() {
		err = errors.New("hub 版本已变化，之前的 node 更新任务已停止；请重新更新")
	} else if info.Version != record.Target && (record.Phase == "restarting" || record.Phase == "confirming") {
		// An accepted install may have rolled back while the hub was down.
		// Do not automatically reinstall a build that just failed to start.
		err = errors.New("node 更新后仍运行旧版，可能已回滚；请检查电脑状态后手动重试")
	} else {
		var bin string
		if info.Version != record.Target {
			bin, err = nodeBinary(info.OS, info.Arch)
		}
		if err == nil {
			err = m.startRPCDeployment(bin, &record)
		}
	}
	if err != nil {
		record.State, record.Phase, record.Error = "failed", "", err.Error()
		_ = m.saveDeployRecord(ctx, record)
		m.mu.Lock()
		m.deployErr = err.Error()
		m.mu.Unlock()
		m.d.changed()
	}
}

func (m *Machine) queueOfflineNodeUpdate(ctx context.Context) error {
	record := nodeDeployRecord{Target: m.d.hubVersion(), State: "queued", Phase: "waiting_idle", At: time.Now().UTC()}
	return m.saveDeployRecord(ctx, record)
}
