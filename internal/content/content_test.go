package content

import (
	"errors"
	"strings"
	"testing"
)

const sample = `<h1>개요</h1><p>이 문서는 <strong>배포</strong> 절차를 설명합니다 &amp; 참고하세요.</p>` +
	`<ac:structured-macro ac:name="info"><ac:rich-text-body><p>주의 사항</p></ac:rich-text-body></ac:structured-macro>` +
	`<h2>절차</h2><ol><li>빌드</li><li>배포<ul><li>스테이징</li></ul></li></ol>` +
	`<ac:structured-macro ac:name="code"><ac:parameter ac:name="language">bash</ac:parameter>` +
	`<ac:plain-text-body><![CDATA[make release && echo "<done>"]]></ac:plain-text-body></ac:structured-macro>` +
	`<ac:structured-macro ac:name="include"><ac:parameter ac:name=""><ac:link><ri:page ri:content-title="비밀 문서" /></ac:link></ac:parameter></ac:structured-macro>` +
	`<table><tbody><tr><th>항목</th><th>값</th></tr><tr><td>버전</td><td>7.2.0</td></tr></tbody></table>` +
	`<h2>부록</h2><p>끝 배포</p>`

func TestToMarkdown(t *testing.T) {
	out, err := ToMarkdown(sample, false)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"# 개요", "**배포**", "& 참고", "**[정보]**", "1. 빌드", "  - 스테이징",
		"```bash", `make release && echo "<done>"`, "| 항목 | 값 |", "| 버전 | 7.2.0 |", "[매크로: include"} {
		if !strings.Contains(out.Text, want) {
			t.Errorf("missing %q in:\n%s", want, out.Text)
		}
	}
	if strings.Contains(out.Text, "비밀 문서") {
		t.Errorf("include target title leaked:\n%s", out.Text)
	}
	if !out.Lossy {
		t.Error("include macro must be reported as a loss")
	}
}

func TestParseRefusesEntities(t *testing.T) {
	// An external entity must not be resolved; a DTD is simply dropped.
	evil := `<!DOCTYPE x [<!ENTITY xxe SYSTEM "file:///etc/passwd">]><p>&xxe;</p>`
	out, err := ToMarkdown(evil, true)
	if err == nil && strings.Contains(out.Text, "root:") {
		t.Fatal("external entity was resolved")
	}
}

func TestReplaceTextKeepsMacros(t *testing.T) {
	out, n := ReplaceText(sample, "배포", "릴리스", true)
	if n != 3 {
		t.Fatalf("replacements = %d, want 3", n)
	}
	if Lost(Take(sample), Take(out)) != nil {
		t.Fatalf("macros lost: %v", Lost(Take(sample), Take(out)))
	}
	if !strings.Contains(out, `<![CDATA[make release && echo "<done>"]]>`) {
		t.Error("code macro body changed")
	}
	if !strings.Contains(out, `ri:content-title="비밀 문서"`) {
		t.Error("include reference changed")
	}
}

func TestSection(t *testing.T) {
	start, end, err := Section(sample, "절차")
	if err != nil {
		t.Fatal(err)
	}
	region := sample[start:end]
	if !strings.HasPrefix(region, "<ol>") || strings.Contains(region, "부록") {
		t.Fatalf("unexpected region: %s", region)
	}
	res, err := ApplyUpdate(sample, UpdateInput{Mode: ModeReplaceSection, Section: "절차", Body: "- 새 단계"})
	if !errors.Is(err, ErrUnsupported) {
		t.Fatalf("expected UNSUPPORTED_CONTENT for losing the code/include macros, got %v (%s)", err, res.Storage)
	}
	res, err = ApplyUpdate(sample, UpdateInput{Mode: ModeReplaceSection, Section: "부록", Body: "새 **부록**"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(res.Storage, "<h2>부록</h2><p>새 <strong>부록</strong></p>") {
		t.Fatalf("section not replaced: %s", res.Storage)
	}
}

func TestReplaceMarkdownBlockedOnStructure(t *testing.T) {
	if _, err := ApplyUpdate(sample, UpdateInput{Mode: ModeReplaceMarkdown, Body: "# x"}); !errors.Is(err, ErrUnsupported) {
		t.Fatalf("want ErrUnsupported, got %v", err)
	}
	if _, err := ApplyUpdate("<p>plain</p>", UpdateInput{Mode: ModeReplaceMarkdown, Body: "# x"}); err != nil {
		t.Fatal(err)
	}
}

func TestReplaceStorageDetectsRemoval(t *testing.T) {
	_, err := ApplyUpdate(sample, UpdateInput{Mode: ModeReplaceStorage, Body: "<p>짧게</p>"})
	if !errors.Is(err, ErrUnsupported) {
		t.Fatalf("want ErrUnsupported, got %v", err)
	}
	res, err := ApplyUpdate(sample, UpdateInput{Mode: ModeReplaceStorage, Body: "<p>짧게</p>", AllowRemovals: true})
	if err != nil || len(res.Warnings) == 0 {
		t.Fatalf("want warning, got %v %v", err, res.Warnings)
	}
}

func TestFromMarkdown(t *testing.T) {
	md := "# 제목\n\n본문 **굵게** _기울임_ `a<b` [링크](https://x.example/a_b) <script>x</script>\n\n" +
		"- 하나\n  - 둘\n- 셋\n\n1. 첫째\n2. 둘째\n\n- [ ] 할 일\n- [x] 완료\n\n```go\nfmt.Println(\"]]>\")\n```\n\n" +
		"| a | b |\n|---|---|\n| 1 | 2 |\n\n> 인용\n\n---\n"
	s := FromMarkdown(md)
	for _, want := range []string{"<h1>제목</h1>", "<strong>굵게</strong>", "<em>기울임</em>", "<code>a&lt;b</code>",
		`<a href="https://x.example/a_b">링크</a>`, "&lt;script&gt;", "<ul><li>하나<ul><li>둘</li></ul></li><li>셋</li></ul>",
		"<ol><li>첫째</li><li>둘째</li></ol>", "<ac:task-status>complete</ac:task-status>",
		`<ac:parameter ac:name="language">go</ac:parameter>`, "]]]]><![CDATA[>", "<th>a</th>", "<td>2</td>",
		"<blockquote><p>인용</p></blockquote>", "<hr />"} {
		if !strings.Contains(s, want) {
			t.Errorf("missing %q in %s", want, s)
		}
	}
	if strings.Contains(s, "<script>") {
		t.Error("raw HTML passed through")
	}
	if _, err := Parse(s); err != nil {
		t.Fatalf("generated storage does not parse: %v", err)
	}
	back, _ := ToMarkdown(s, false)
	if !strings.Contains(back.Text, `fmt.Println("]]>")`) {
		t.Errorf("code round trip failed:\n%s", back.Text)
	}
}

func TestWindow(t *testing.T) {
	c := Window("가나다라마", 1, 2)
	if c.Text != "나다" || !c.Truncated || *c.NextOffset != 3 || c.TotalChars != 5 {
		t.Fatalf("%+v", c)
	}
}

func TestDiff(t *testing.T) {
	d, stats := Diff("<p>a</p><p>b</p>", "<p>a</p><p>c</p>", 1)
	if !strings.Contains(d, "- <p>b</p>") || !strings.Contains(d, "+ <p>c</p>") || stats["added"] != 1 {
		t.Fatalf("%s %v", d, stats)
	}
}
