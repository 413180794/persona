package kb

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/chyroc/jarvis/server/internal/ai"
	"github.com/chyroc/jarvis/server/internal/prompts"
)

// Project dedupe (docs/knowledge.md「项目去重」). The same work is often discussed in several groups under different
// names, and a chat's compression only sees project names: each group grew its own project (2026-11-01: SS codex 方案
// and SS 支持单 agent 多 Session 窗口, 10 people in common, no word in common). Pairs of live projects that share
// people, chats or name words go to the model with both profiles; the ones it calls the same are merged. Each pair's
// verdict is kept (projects/.dedupe.json) so it is asked once, and again only after either side changed a lot.

const (
	// dedupeRecheck: a pair judged different is asked again after this long (both have grown since).
	dedupeRecheck = 14 * 24 * time.Hour
	// dedupeMinShared is the fewest people in common for a pair to be a candidate on people alone.
	dedupeMinShared = 3
)

type dedupeVerdict struct {
	Same   bool      `json:"same"`
	Reason string    `json:"reason,omitempty"`
	At     time.Time `json:"at"`
}

func (s *Store) dedupePath() string { return filepath.Join(s.Dir, KindProject, ".dedupe.json") }

func (s *Store) dedupeVerdicts() map[string]dedupeVerdict {
	out := map[string]dedupeVerdict{}
	if raw, err := os.ReadFile(s.dedupePath()); err == nil {
		_ = json.Unmarshal(raw, &out)
	}
	return out
}

func pairKey(a, b string) string {
	if a > b {
		a, b = b, a
	}
	return a + "|" + b
}

// dedupeCandidate is a pair worth asking about, with how strongly it looks like one project.
type dedupeCandidate struct {
	a, b   *Project
	shared int
	score  float64
}

// dedupeCandidates ranks pairs of live, not ignored projects by what they have in common: people (relative to the
// smaller side), chats, documents, and words of their names and aliases.
func dedupeCandidates(projects []*Project) []dedupeCandidate {
	var live []*Project
	for _, p := range projects {
		if p.Live() && p.Status != "ignored" {
			live = append(live, p)
		}
	}
	var out []dedupeCandidate
	for i := 0; i < len(live); i++ {
		for j := i + 1; j < len(live); j++ {
			a, b := live[i], live[j]
			shared := overlap(a.People, b.People)
			chats := overlap(a.Chats, b.Chats)
			items := overlap(a.Items, b.Items)
			words := overlap(nameWords(a), nameWords(b))
			smaller := min(len(a.People), len(b.People))
			// Everyone is in a few big groups: people alone count only when it is a good part of the smaller side.
			byPeople := shared >= dedupeMinShared && smaller > 0 && float64(shared)/float64(smaller) >= 0.3
			if !byPeople && chats == 0 && items == 0 && words < 2 {
				continue
			}
			score := float64(chats)*3 + float64(items) + float64(words)*2
			if smaller > 0 {
				score += 5 * float64(shared) / float64(smaller)
			}
			out = append(out, dedupeCandidate{a: a, b: b, shared: shared, score: score})
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].score != out[j].score {
			return out[i].score > out[j].score
		}
		return pairKey(out[i].a.Slug, out[i].b.Slug) < pairKey(out[j].a.Slug, out[j].b.Slug)
	})
	return out
}

func overlap(a, b []string) int {
	set := map[string]bool{}
	for _, x := range a {
		set[x] = true
	}
	n := 0
	seen := map[string]bool{}
	for _, x := range b {
		if set[x] && !seen[x] {
			seen[x] = true
			n++
		}
	}
	return n
}

// nameWords are the words of a project's name and aliases, lowercased, two characters or more: "SS codex 方案" →
// ss, codex, 方案. Common words every project has say nothing and are left out.
func nameWords(p *Project) []string {
	var out []string
	for _, n := range append([]string{p.Name}, p.Aliases...) {
		for _, w := range strings.Split(Slug(n), "-") {
			if len([]rune(w)) >= 2 && !genericWord[w] {
				out = addUnique(out, w)
			}
		}
	}
	return out
}

var genericWord = map[string]bool{"项目": true, "方案": true, "需求": true, "支持": true, "优化": true, "改版": true, "上线": true,
	"迭代": true, "联调": true, "接入": true, "agent": true, "ai": true, "the": true, "and": true}

// DedupeProjects asks about at most max candidate pairs not judged yet (or judged different a while ago) and merges
// the ones the model calls the same. Returns how many were merged.
func (e *Engine) DedupeProjects(ctx context.Context, max int) int {
	if e.Model == nil {
		return 0
	}
	verdicts := e.Store.dedupeVerdicts()
	defer func() {
		raw, _ := json.MarshalIndent(verdicts, "", "  ")
		_ = writeFile(e.Store.dedupePath(), append(raw, '\n'))
	}()
	asked, merged := 0, 0
	gone := map[string]bool{} // merged away this run: their other pairs are stale
	for _, c := range dedupeCandidates(e.Store.Projects()) {
		if asked >= max || ctx.Err() != nil {
			break
		}
		if gone[c.a.Slug] || gone[c.b.Slug] {
			continue
		}
		key := pairKey(c.a.Slug, c.b.Slug)
		if v, ok := verdicts[key]; ok && (v.Same || time.Since(v.At) < dedupeRecheck) {
			continue
		}
		asked++
		same, keepA, reason, err := e.judgeDuplicate(ctx, c)
		if err != nil {
			if ai.IsPaused(err) { // not judged: asked again once the connection is back
				break
			}
			e.logf("dedupe %s: %v", key, err)
			continue
		}
		verdicts[key] = dedupeVerdict{Same: same, Reason: reason, At: time.Now().UTC()}
		if !same {
			continue
		}
		keep, drop := c.a, c.b
		if !keepA {
			keep, drop = c.b, c.a
		}
		// A project named or tagged by hand stays: the other one goes into it.
		if drop.Locked && !keep.Locked {
			keep, drop = drop, keep
		}
		if err := e.MergeProjects(keep.Slug, drop.Slug, "去重 · "+reason); err != nil {
			e.logf("dedupe merge %s → %s: %v", drop.Slug, keep.Slug, err)
			continue
		}
		e.logf("dedupe: merged %s into %s (%s)", drop.Slug, keep.Slug, reason)
		gone[drop.Slug] = true
		merged++
	}
	return merged
}

func (e *Engine) judgeDuplicate(ctx context.Context, c dedupeCandidate) (same, keepA bool, reason string, err error) {
	side := func(key string, p *Project) prompts.KBDedupeProject {
		var chats []string
		for i, id := range p.Chats {
			if i >= 6 {
				break
			}
			if ch, ok := e.Store.Chat(id); ok && ch.Name != "" {
				chats = append(chats, ch.Name)
			}
		}
		return prompts.KBDedupeProject{Key: key, Name: p.Name, Aliases: p.Aliases, OneLine: p.OneLine, Goal: p.Goal,
			Summary: clip(e.Store.Summary(KindProject, p.Slug), 1500), Chats: chats, People: len(p.People)}
	}
	in := prompts.RenderKBDedupeInput(prompts.KBDedupeInput{A: side("A", c.a), B: side("B", c.b), Shared: c.shared})
	var d struct {
		Same   bool   `json:"same"`
		Keep   string `json:"keep"`
		Reason string `json:"reason"`
	}
	cctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	if err := e.modelFor("kb.dedupe").JSONMax(cctx, prompts.RenderKBDedupe(prompts.KBOwner{Owner: e.Owner}), in, 600, &d); err != nil {
		return false, false, "", err
	}
	return d.Same, !strings.EqualFold(strings.TrimSpace(d.Keep), "B"), oneLine(d.Reason), nil
}

// MergeProjects folds drop into keep: drop's name and aliases become keep's aliases; people, chats, documents,
// code directories, keywords and removed posts are joined; the timelines are interleaved; the chats, people and items
// that named drop name keep. drop stays on disk as merged (merged_into), hidden from lists, so it can be undone by
// hand. keep is marked dirty: its profile is rewritten from both.
func (e *Engine) MergeProjects(keepSlug, dropSlug, why string) error {
	if keepSlug == dropSlug {
		return errors.New("a project cannot be merged into itself")
	}
	keep, ok := e.Store.Project(keepSlug)
	if !ok || !keep.Live() {
		return fmt.Errorf("%s: %w", keepSlug, ErrNotFound)
	}
	drop, ok := e.Store.Project(dropSlug)
	if !ok || !drop.Live() {
		return fmt.Errorf("%s: %w", dropSlug, ErrNotFound)
	}
	for _, a := range append([]string{drop.Name}, drop.Aliases...) {
		if a = strings.TrimSpace(a); a != "" && normName(a) != normName(keep.Name) {
			keep.Aliases = addUnique(keep.Aliases, a)
		}
	}
	keep.People = addUnique(keep.People, drop.People...)
	keep.Chats = addUnique(keep.Chats, drop.Chats...)
	keep.Items = addUnique(keep.Items, drop.Items...)
	keep.Dirs = addUnique(keep.Dirs, drop.Dirs...)
	keep.Keywords = addUnique(keep.Keywords, drop.Keywords...)
	keep.Moments = mergeMoments(keep.Moments, drop.Moments)
	if drop.Kind == "long" {
		keep.Kind = "long"
	}
	if keep.Goal == "" {
		keep.Goal = drop.Goal
	}
	if keep.MyRole == "" {
		keep.MyRole = drop.MyRole
	}
	if !keep.Locked {
		if rank(drop.Importance) < rank(keep.Importance) {
			keep.Importance = drop.Importance
		}
		keep.Relevance = max(keep.Relevance, drop.Relevance)
		for _, t := range drop.Tags {
			if len(keep.Tags) < 6 {
				keep.Tags = addUnique(keep.Tags, t)
			}
		}
	}
	if keep.Status == "done" && drop.Status == "active" {
		keep.Status = "active"
	}
	if drop.Created.Before(keep.Created) {
		keep.Created = drop.Created
	}
	if err := e.mergeLogs(keep.Slug, drop.Slug); err != nil {
		return err
	}
	_ = e.Store.AppendLog(keep.Slug, time.Now(), "Jarvis", fmt.Sprintf("合并了项目「%s」（%s）", drop.Name, why))
	keep.Dirty = true
	if err := e.Store.PutProject(keep); err != nil {
		return err
	}
	drop.Status, drop.MergedInto, drop.Dirty = "merged", keep.Slug, false
	if err := e.Store.PutProject(drop); err != nil {
		return err
	}
	// Who named drop names keep.
	swap := func(list []string) ([]string, bool) {
		changed := false
		var out []string
		for _, s := range list {
			if s == drop.Slug {
				s, changed = keep.Slug, true
			}
			out = addUnique(out, s)
		}
		return out, changed
	}
	for _, id := range drop.Chats {
		if c, ok := e.Store.Chat(id); ok {
			if l, ch := swap(c.Projects); ch {
				c.Projects = l
				_ = e.Store.PutChat(c)
			}
		}
	}
	for _, id := range drop.People {
		if p, ok := e.Store.Person(id); ok {
			if l, ch := swap(p.Projects); ch {
				p.Projects = l
				_ = e.Store.PutPerson(p)
			}
		}
	}
	for _, ref := range drop.Items {
		kind, id, ok := strings.Cut(ref, "/")
		if !ok {
			continue
		}
		if it, ok := e.Store.Item(kind, id); ok {
			if l, ch := swap(it.Projects); ch {
				it.Projects = l
				_ = e.Store.PutItem(it)
			}
		}
	}
	e.index(KindProject, keep.Slug, keep.Name, keep.Tagging, keep.OneLine, keep.Updated)
	return nil
}

// rank orders importance: P0 is 0, unset 4.
func rank(p string) int {
	if importanceRe.MatchString(p) {
		return int(p[1] - '0')
	}
	return 4
}

// mergeLogs rewrites keep's timeline with drop's lines in it, by time (the lines start with "- 2006-01-02 15:04").
func (e *Engine) mergeLogs(keepSlug, dropSlug string) error {
	lines := e.Store.Log(keepSlug, math.MaxInt) // both in the archive's zone
	other := e.Store.Log(dropSlug, math.MaxInt)
	if len(other) == 0 {
		return nil
	}
	seen := map[string]bool{}
	for _, l := range lines {
		seen[l] = true
	}
	for _, l := range other {
		if !seen[l] {
			lines = append(lines, l)
		}
	}
	sort.SliceStable(lines, func(i, j int) bool { return logTime(lines[i]) < logTime(lines[j]) })
	return e.Store.WriteLog(keepSlug, lines)
}

func logTime(l string) string {
	if r := []rune(l); len(r) >= 18 {
		return string(r[2:18])
	}
	return ""
}

func mergeMoments(a, b []ProjectMoment) []ProjectMoment {
	seen := map[string]bool{}
	var out []ProjectMoment
	for _, m := range append(append([]ProjectMoment{}, a...), b...) {
		if !seen[m.ID] {
			seen[m.ID] = true
			out = append(out, m)
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].At.After(out[j].At) })
	return out[:min(len(out), maxProjectMoments)]
}
