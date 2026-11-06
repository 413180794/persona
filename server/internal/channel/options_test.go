package channel

import (
	"testing"

	"github.com/nwverse/persona/server/internal/proto"
)

func question(multi bool, labels ...string) proto.Question {
	q := proto.Question{Question: "?", MultiSelect: multi}
	for _, l := range labels {
		q.Options = append(q.Options, proto.QuestionOption{Label: l, Description: "about " + l})
	}
	return q
}

// An option is named by its number, an ordinal in any of the three languages, or its label (case, spaces and
// punctuation aside, held in a sentence that says nothing else, or the start of a longer label).
func TestMatchOptionOne(t *testing.T) {
	db := question(false, "PostgreSQL (recommended)", "SQLite", "MySQL")
	for text, want := range map[string]string{
		// Numbers and ordinals.
		"2": "SQLite", "２": "SQLite", "2。": "SQLite", " 2 ": "SQLite",
		"第二个": "SQLite", "第2个": "SQLite", "第２个": "SQLite", "二": "SQLite", "第二": "SQLite", "选项二": "SQLite",
		"我选第二个吧": "SQLite", "第二个。": "SQLite", "第二种": "SQLite",
		"second": "SQLite", "Second.": "SQLite", "2nd": "SQLite", "the second one": "SQLite", "option 2": "SQLite",
		"number two": "SQLite", "I'll take the second": "SQLite", "I’ll take the second one please": "SQLite",
		"2番目": "SQLite", "二つ目": "SQLite", "2番目で": "SQLite",
		"最后一个": "MySQL", "the last one": "MySQL", "Last": "MySQL", "最後": "MySQL", "第三个": "MySQL", "third": "MySQL",
		"第一个": "PostgreSQL (recommended)", "first": "PostgreSQL (recommended)", "一": "PostgreSQL (recommended)",
		// Labels.
		"sqlite": "SQLite", "SQLite": "SQLite", " SQLite。": "SQLite", "Sq Lite": "SQLite", "ＳＱＬｉｔｅ": "SQLite",
		"用 SQLite 吧": "SQLite", "用SQLite吧": "SQLite", "I'd go with sqlite": "SQLite", "SQLiteで": "SQLite",
		"mysql": "MySQL", "Postgres": "PostgreSQL (recommended)", "postgresql": "PostgreSQL (recommended)",
		"PostgreSQL (recommended)": "PostgreSQL (recommended)",
	} {
		if got, ok := MatchOption(db, text); !ok || got != want {
			t.Errorf("%q: %q, %v; want %q", text, got, ok, want)
		}
	}
	for _, text := range []string{
		"", " ", "。", // nothing said
		"4", "４", "01", "第四个", "fourth", "第零个", // no such option
		"SQL",                                      // in every label
		"用 MariaDB 吧", "whatever is fastest", "都行", // a free answer
		"不要 SQLite", "don't use sqlite", "not the second one", "SQLite 以外", // turned down
		"我觉得用 SQLite 会更好一些", "sqlite, and also add an index on the users table", // more said than the option
		"1 2", "SQLite 和 MySQL", "第一个和第三个", // two of a single choice
	} {
		if got, ok := MatchOption(db, text); ok {
			t.Errorf("%q: %q, but it names no one option", text, got)
		}
	}
	if _, ok := MatchOption(proto.Question{Question: "?"}, "1"); ok {
		t.Error("a question without options has an option")
	}
}

// A label inside another (Java, JavaScript), a short label (Go in "good", a letter in a sentence).
func TestMatchOptionLabelsThatLookAlike(t *testing.T) {
	lang := question(false, "Java", "JavaScript", "Go")
	for text, want := range map[string]string{"用 JavaScript": "JavaScript", "javascript 吧": "JavaScript", "java": "Java",
		"用 Java 吧": "Java", "go": "Go", "用 Go 吧": "Go", "let's use go": "Go"} {
		if got, ok := MatchOption(lang, text); !ok || got != want {
			t.Errorf("%q: %q, %v; want %q", text, got, ok, want)
		}
	}
	for _, text := range []string{"good choice", "golang"} {
		if got, ok := MatchOption(lang, text); ok {
			t.Errorf("%q: %q", text, got)
		}
	}
	letters := question(false, "A", "B")
	if got, ok := MatchOption(letters, "b"); !ok || got != "B" {
		t.Errorf("b: %q, %v", got, ok)
	}
	if got, ok := MatchOption(letters, "a bit of both"); ok {
		t.Errorf("a letter in a sentence: %q", got)
	}
	if got, ok := MatchOption(question(false, "C++", "Rust"), "write it in c"); ok {
		t.Errorf("C++ is not c: %q", got)
	}
	if got, ok := MatchOption(question(false, "Postgres", "SQLite"), "SQLite，SQLite"); !ok || got != "SQLite" {
		t.Errorf("said twice: %q, %v", got, ok)
	}
}

// A bare number is the option's number while there is one (the text channels ask for numbers), unless another option's
// label is that number: then which was meant is the agent's to read, from the answer itself. Past the options, a label.
func TestMatchOptionNumbersAsLabels(t *testing.T) {
	for _, c := range []struct {
		q    proto.Question
		want map[string]string // "" for a free answer
	}{
		{question(false, "2", "4", "8"), map[string]string{"2": "", "二": "", "two": "", "3": "8", "8": "8", "4": "4",
			"第二个": "4", "second": "4", "option 2": "4", "第一个": "2"}},
		{question(false, "1", "3", "5"), map[string]string{"3": "", "三": "", "three": "", "1": "1", "2": "3", "5": "5"}},
		{question(false, "2 workers", "4 workers", "8 workers"), map[string]string{"2": "", "3": "8 workers",
			"2 workers": "2 workers", "用 4 workers 吧": "4 workers"}},
		{question(false, "三个副本", "一个副本"), map[string]string{"1": "", "2": "一个副本", "第一个": "三个副本"}},
		// Numbers that do not make the label a number: numbered labels, a decimal, a numeral that counts nothing.
		{question(false, "1. Fix", "2. Rewrite"), map[string]string{"1": "1. Fix", "2": "2. Rewrite"}},
		{question(false, "Normal", "1.5x"), map[string]string{"1": "Normal", "2": "1.5x"}},
		{question(false, "一键部署", "手动部署"), map[string]string{"1": "一键部署", "二": "手动部署"}},
	} {
		for text, want := range c.want {
			got, ok := MatchOption(c.q, text)
			if want == "" && ok || want != "" && (!ok || got != want) {
				t.Errorf("%v %q: %q, %v; want %q", c.q.Options, text, got, ok, want)
			}
		}
	}
}

// A count ("1 of", "one of" in Chinese) is not a place: a measure word takes the ordinal prefix before the numeral; without it, it may be a label.
func TestMatchOptionCountsAreNotPlaces(t *testing.T) {
	for _, c := range []struct {
		q    proto.Question
		want map[string]string // "" for a free answer
	}{
		{question(false, "3 个副本", "1 个副本", "5 个副本"), map[string]string{"1个": "1 个副本", "1 个": "1 个副本",
			"第1个": "3 个副本", "第一个": "3 个副本"}},
		{question(false, "两个", "一个"), map[string]string{"要一个": "一个", "一个": "一个", "第一个": "两个"}},
		{question(false, "Postgres", "SQLite"), map[string]string{"一个": "", "1个": "", "第一个": "Postgres", "一": "Postgres"}},
	} {
		for text, want := range c.want {
			got, ok := MatchOption(c.q, text)
			if want == "" && ok || want != "" && (!ok || got != want) {
				t.Errorf("%v %q: %q, %v; want %q", c.q.Options, text, got, ok, want)
			}
		}
	}
}

// A label in a sentence is that option only when nothing else is said but filler words (Chinese "use", a closing particle, I'd go with, Japanese "with"): a
// condition or an instruction besides it ("SQLite, encrypted", "delete after a backup") is the answer itself, every word of it.
func TestMatchOptionHeldLabelOnlyWithFiller(t *testing.T) {
	for _, c := range []struct {
		q    proto.Question
		want map[string]string // "" for a free answer
	}{
		{question(false, "PostgreSQL", "SQLite", "MySQL"), map[string]string{
			"用 SQLite 吧": "SQLite", "就用 SQLite 吧": "SQLite", "SQLite 就行": "SQLite", "那就 SQLite 吧": "SQLite",
			"选 SQLite 好了": "SQLite", "I'd go with sqlite": "SQLite", "let's use sqlite please": "SQLite",
			"SQLiteで": "SQLite", "SQLiteでお願いします": "SQLite", "SQLiteにします": "SQLite", "SQLiteを使う": "SQLite",
			"SQLite, but encrypted": "", "SQLite but encrypted": "", "SQLite in WAL mode": "", "MySQL 8.0": "",
			"PostgreSQL with replicas": "", "SQLite 加密": "", "SQLite，加密": "", "SQLite for now": "",
			"SQLite 先试试": "", "SQLiteで暗号化": "",
		}},
		{question(false, "删除", "保留"), map[string]string{"删除": "删除", "那就删除吧": "删除", "备份后删除": "", "删除日志": ""}},
		{question(false, "部署", "取消"), map[string]string{"部署吧": "部署", "明天部署": ""}},
		{question(false, "合并", "关闭"), map[string]string{"合并": "合并", "改完再合并": ""}},
		{question(false, "打补丁", "重写"), map[string]string{"打补丁": "打补丁", "就打补丁吧": "打补丁", "打补丁加测试": ""}},
		{question(false, "Approve", "Reject"), map[string]string{"approve": "Approve", "approve but rename it": ""}},
		{question(false, "React", "Vue"), map[string]string{"react": "React", "React with TypeScript": ""}},
		{question(false, "Postgres", "Other"), map[string]string{"Other": "Other", "Other: use mongo": ""}},
		{question(false, "Use the cache", "Rebuild"), map[string]string{"use the cache please": "Use the cache",
			"use the cache for tests only": ""}},
	} {
		for text, want := range c.want {
			got, ok := MatchOption(c.q, text)
			if want == "" && ok || want != "" && (!ok || got != want) {
				t.Errorf("%v %q: %q, %v; want %q", c.q.Options, text, got, ok, want)
			}
		}
	}
}

// What a label starts with is that option ("Postgres" of PostgreSQL), from a word's start for ASCII and the label's
// for Chinese and Japanese; a piece inside a word, a function word, or a word saying how many is not.
func TestMatchOptionStartOfALabel(t *testing.T) {
	for _, c := range []struct {
		q    proto.Question
		want map[string]string // "" for a free answer
	}{
		{question(false, "PostgreSQL (recommended)", "SQLite"), map[string]string{"postgres": "PostgreSQL (recommended)",
			"postgre sql": "PostgreSQL (recommended)", "recommended": "PostgreSQL (recommended)"}},
		{question(false, "Use the cache", "Rebuild"), map[string]string{"the cache": "Use the cache", "the": "",
			"rebuild": "Rebuild", "re": ""}},
		{question(true, "Unit tests", "Lint", "Run in parallel"), map[string]string{"all": "", "unit": "Unit tests",
			"parallel": "Run in parallel", "int": "", "llel": ""}},
		{question(false, "Small fix", "Big refactor"), map[string]string{"all": "", "small": "Small fix"}},
		{question(false, "Many small PRs", "One big PR"), map[string]string{"any": "", "many": "Many small PRs"}},
		{question(false, "Refine the prompt", "Keep it"), map[string]string{"fine": "", "refine": "Refine the prompt"}},
		{question(false, "Approve", "Reject"), map[string]string{"prove": "", "appro": "Approve"}},
		{question(false, "Known issue", "New bug"), map[string]string{"now": "", "known": "Known issue"}},
		{question(false, "Install dependencies", "Skip"), map[string]string{"end": "", "install": "Install dependencies"}},
		{question(false, "Latest version", "Pinned"), map[string]string{"test": "", "latest": "Latest version"}},
		{question(false, "Reuse cache", "Rebuild"), map[string]string{"use": ""}},
		{question(true, "全部删除", "保留"), map[string]string{"全部": "", "保留": "保留"}},
		{question(false, "单元测试", "集成测试"), map[string]string{"单元": "单元测试", "集成": "集成测试", "测试": ""}},
		{question(false, "先备份再删除", "保留"), map[string]string{"先备份": "先备份再删除", "再删除": ""}},
	} {
		for text, want := range c.want {
			got, ok := MatchOption(c.q, text)
			if want == "" && ok || want != "" && (!ok || got != want) {
				t.Errorf("%v %q: %q, %v; want %q", c.q.Options, text, got, ok, want)
			}
		}
	}
}

// A question mark asks about an option rather than takes it ("2?" never was a choice): "SQLite?", "the second?" are free
// answers, unless the label is itself a question said as it is.
func TestMatchOptionQuestionMarks(t *testing.T) {
	db := question(false, "PostgreSQL", "SQLite", "MySQL")
	for _, text := range []string{"2?", "２？", "第二个？", "second?", "the second one?", "SQLite?", "SQLite？", "sqlite ?",
		"用 SQLite 吗?", "SQLite?。", "Postgres?"} {
		if got, ok := MatchOption(db, text); ok {
			t.Errorf("%q: %q, but it asks", text, got)
		}
	}
	multi := question(true, "进展", "风险", "计划")
	for _, text := range []string{"1, 2?", "1 2 ?", "进展、风险？", "风险？, 进展"} {
		if got, ok := MatchOption(multi, text); ok {
			t.Errorf("%q: %q, but it asks", text, got)
		}
	}
	if got, ok := MatchOption(question(false, "Why not both?", "Pick one"), "why not both?"); !ok || got != "Why not both?" {
		t.Errorf("a label that asks: %q, %v", got, ok)
	}
}

// A multi-select takes several, split on commas (full-width and enumeration commas too), the Chinese "and" and "and" (and slashes, spaces between numbers), in the order said; a label
// holding one of those is still one option.
func TestMatchOptionSeveral(t *testing.T) {
	parts := question(true, "进展", "风险", "计划")
	for text, want := range map[string]string{
		"1 3": "进展, 计划", "１，３": "进展, 计划", "1，3 。": "进展, 计划", "1、3": "进展, 计划",
		"进展、计划": "进展, 计划", "风险，进展": "风险, 进展", "进展和计划": "进展, 计划", "第一个和第三个": "进展, 计划",
		"一和三": "进展, 计划", "进展 和 风险 和 计划": "进展, 风险, 计划", "1 1": "进展", "风险": "风险", "第二个": "风险",
		"进展,风险;计划": "进展, 风险, 计划",
	} {
		if got, ok := MatchOption(parts, text); !ok || got != want {
			t.Errorf("%q: %q, %v; want %q", text, got, ok, want)
		}
	}
	en := question(true, "Tests", "Docs", "Release notes")
	for text, want := range map[string]string{"first and third": "Tests, Release notes", "tests and docs": "Tests, Docs",
		"Docs, release notes": "Docs, Release notes", "the second and the third": "Docs, Release notes", "Tests & Docs": "Tests, Docs",
		"TestsとDocs": "Tests, Docs"} {
		if got, ok := MatchOption(en, text); !ok || got != want {
			t.Errorf("%q: %q, %v; want %q", text, got, ok, want)
		}
	}
	and := question(true, "测试和文档", "代码", "Bugs and fixes")
	for text, want := range map[string]string{"测试和文档": "测试和文档", "测试和文档、代码": "测试和文档, 代码", "bugs and fixes": "Bugs and fixes",
		"代码, bugs and fixes": "代码, Bugs and fixes"} {
		if got, ok := MatchOption(and, text); !ok || got != want {
			t.Errorf("%q: %q, %v; want %q", text, got, ok, want)
		}
	}
	slash := question(true, "A/B test", "Ship", "CI/CD")
	for text, want := range map[string]string{"A/B test, CI/CD": "A/B test, CI/CD", "A/B test 和 Ship": "A/B test, Ship",
		"ci/cd": "CI/CD", "Ship、CI/CD": "Ship, CI/CD", "1/3": "A/B test, CI/CD", "ab test and ci cd": "A/B test, CI/CD"} {
		if got, ok := MatchOption(slash, text); !ok || got != want {
			t.Errorf("%q: %q, %v; want %q", text, got, ok, want)
		}
	}
	if got, ok := MatchOption(en, "Tests/Docs"); !ok || got != "Tests, Docs" {
		t.Errorf("Tests/Docs: %q, %v", got, ok)
	}
	for _, text := range []string{"进展和别的", "1 4", "进展、"} {
		got, ok := MatchOption(parts, text)
		if text == "进展、" {
			if !ok || got != "进展" {
				t.Errorf("%q: %q, %v (a stray comma)", text, got, ok)
			}
			continue
		}
		if ok {
			t.Errorf("%q: %q, but one piece is no option", text, got)
		}
	}
}

// A question answered in a text channel takes the same words: an ordinal or a label becomes the label, anything else
// is the answer itself.
func TestReadAnswerTakesOrdinalsAndLabels(t *testing.T) {
	ix := proto.Interaction{Kind: proto.IxQuestion, Questions: []proto.Question{
		question(false, "Postgres", "SQLite"), {Question: "备注？"}}}
	ix.Questions[0].Question = "数据库？"
	a := NewAsk("wechat", ix, t0)
	if _, done, _ := ReadAnswer("", a, "第二个"); done {
		t.Fatal("done after the first of two")
	}
	r, done, _ := ReadAnswer("", a, "第二个")
	if !done || r.Answers["数据库？"] != "SQLite" || r.Answers["备注？"] != "第二个" {
		t.Fatalf("%+v", r)
	}
	b := NewAsk("wechat", ix, t0)
	ReadAnswer("", b, "用 MySQL 吧")
	if b.Answers["数据库？"] != "用 MySQL 吧" {
		t.Fatalf("a free answer: %+v", b.Answers)
	}
}

// A phone keyboard shapes a number as a circled 1, a full-width (1), 1), and Chinese says a bare "No. 2": all name the option. A question
// mark, a shape out of range, a counting measure word, and a label that is itself the number keep the usual rules.
func TestMatchOptionNumberShapes(t *testing.T) {
	db := question(false, "PostgreSQL", "SQLite", "MySQL")
	for text, want := range map[string]string{
		"①": "PostgreSQL", "②": "SQLite", "③": "MySQL", "②。": "SQLite",
		"（1）": "PostgreSQL", "(2)": "SQLite", "【3】": "MySQL", "[1]": "PostgreSQL",
		"「2」": "SQLite", "『3』": "MySQL", "1)": "PostgreSQL", "2）": "SQLite",
		"二号": "SQLite", "2号": "SQLite", "三号": "MySQL", "选二号": "SQLite", "二号吧": "SQLite", "2号吧": "SQLite",
	} {
		if got, ok := MatchOption(db, text); !ok || got != want {
			t.Errorf("%q: %q, %v; want %q", text, got, ok, want)
		}
	}
	multi := question(true, "进展", "风险", "计划")
	for text, want := range map[string]string{"①，③": "进展, 计划", "二号和三号": "风险, 计划"} {
		if got, ok := MatchOption(multi, text); !ok || got != want {
			t.Errorf("%q: %q, %v; want %q", text, got, ok, want)
		}
	}
	for _, text := range []string{"①？", "①?", "（1）?", "1)？", "④", "（4）", "⑵", "⓪", "（01）", "二号？", "一个", "二个", "1个", "2个"} {
		if got, ok := MatchOption(db, text); ok {
			t.Errorf("%q: %q, but it is not a choice", text, got)
		}
	}
	// Another option's label is the number 2: a bare "No. 2" is then ambiguous and stays the answer itself.
	collide := question(false, "2", "4", "8")
	if got, ok := MatchOption(collide, "2号"); ok {
		t.Errorf("2号 with a label 2: %q, %v", got, ok)
	}
}

// The words that approve or refuse a card, in any language, with the marks a reply may end with; anything more is an
// instruction.
func TestIsApprovalWord(t *testing.T) {
	for _, text := range []string{"批准", "好的。", "OK", "approve!", "承認", "不行", "deny", "いいえ"} {
		if !IsApprovalWord(text) {
			t.Errorf("%q is an approval word", text)
		}
	}
	for _, text := range []string{"批准后再跑一遍测试", "好的，顺便把 lint 也修了", "run the tests", "第二个", ""} {
		if IsApprovalWord(text) {
			t.Errorf("%q is not an approval word", text)
		}
	}
}
